package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v6"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v6/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v6/search"
	"github.com/spf13/viper"
)

type Config struct {
	AzureDevOps struct {
		Organization string `mapstructure:"organization"`
		Project      string `mapstructure:"project"`
		PAT          string `mapstructure:"pat"`
		APIVersion   string `mapstructure:"api_version"`
	} `mapstructure:"azure_devops"`
	Server struct {
		Port int    `mapstructure:"port"`
		Host string `mapstructure:"host"`
	} `mapstructure:"server"`
}

type AzureDevOpsClient struct {
	config       *Config
	connection   *azuredevops.Connection
	gitClient    git.Client
	searchClient search.Client
}

func NewAzureDevOpsClient() (*AzureDevOpsClient, error) {
	var config Config
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Error reading config: %v", err)
		return nil, fmt.Errorf("error reading config: %w", err)
	}

	if err := viper.Unmarshal(&config); err != nil {
		log.Printf("Error unmarshaling config: %v", err)
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Allow PAT override from environment variable
	if pat := os.Getenv("AZURE_DEVOPS_PAT"); pat != "" {
		config.AzureDevOps.PAT = pat
	}

	if config.AzureDevOps.PAT == "" {
		log.Print("Azure DevOps PAT is required")
		return nil, fmt.Errorf("Azure DevOps PAT is required")
	}

	// Create Azure DevOps connection
	organizationURL := fmt.Sprintf("https://dev.azure.com/%s", config.AzureDevOps.Organization)
	connection := azuredevops.NewPatConnection(organizationURL, config.AzureDevOps.PAT)

	// Create Git client
	gitClient, err := git.NewClient(context.Background(), connection)
	if err != nil {
		log.Printf("Failed to create git client: %v", err)
		return nil, fmt.Errorf("failed to create git client: %w", err)
	}

	// Create Search client
	searchClient, err := search.NewClient(context.Background(), connection)
	if err != nil {
		log.Printf("Failed to create search client: %v", err)
		return nil, fmt.Errorf("failed to create search client: %w", err)
	}

	return &AzureDevOpsClient{
		config:       &config,
		connection:   connection,
		gitClient:    gitClient,
		searchClient: searchClient,
	}, nil
}

func (c *AzureDevOpsClient) searchRepository(ctx context.Context, query string, repoName string) ([]map[string]interface{}, error) {
	// Create search request
	filters := make(map[string][]string)
	filters["Project"] = []string{c.config.AzureDevOps.Project}
	if repoName != "" {
		filters["Repository"] = []string{repoName}
	}

	includeSnippet := true

	searchRequest := &search.CodeSearchRequest{
		SearchText:     &query,
		Filters:        &filters,
		IncludeSnippet: &includeSnippet,
		Top:            &[]int{1000}[0],
	}
	// Call search API
	response, err := c.searchClient.FetchCodeSearchResults(ctx, search.FetchCodeSearchResultsArgs{
		Project: &c.config.AzureDevOps.Project,
		Request: searchRequest,
	})
	if err != nil {
		log.Printf("Error searching code: %v", err)
		return nil, fmt.Errorf("error searching code: %w", err)
	}

	// Process results
	results := []map[string]interface{}{}
	if response != nil && response.Results != nil {
		for _, result := range *response.Results {
			if result.Repository == nil || result.Path == nil || result.FileName == nil {
				continue
			}
			results = append(results, map[string]interface{}{
				"repository": *result.Repository.Name,
				"path":       *result.Path,
				"fileName":   *result.FileName,
				"project":    *result.Project.Name,
			})
		}
	}

	return results, nil
}

func (c *AzureDevOpsClient) getFileContent(ctx context.Context, repoName, path string) (string, error) {
	repos, err := c.gitClient.GetRepositories(ctx, git.GetRepositoriesArgs{
		Project: &c.config.AzureDevOps.Project,
	})
	if err != nil {
		log.Printf("Error getting repositories: %v", err)
		return "", err
	}

	var targetRepo *git.GitRepository
	for _, repo := range *repos {
		if strings.EqualFold(*repo.Name, repoName) {
			targetRepo = &repo
			break
		}
	}

	if targetRepo == nil {
		log.Printf("Repository not found: %s", repoName)
		return "", fmt.Errorf("repository not found: %s", repoName)
	}

	repoID := targetRepo.Id.String()

	item, err := c.gitClient.GetItem(ctx, git.GetItemArgs{
		RepositoryId:   &repoID,
		Project:        &c.config.AzureDevOps.Project,
		Path:           &path,
		IncludeContent: &[]bool{true}[0],
	})
	if err != nil {
		log.Printf("Error getting file content: %v", err)
		return "", err
	}

	if item.Content == nil {
		return "", nil
	}

	return *item.Content, nil
}

func main() {
	client, err := NewAzureDevOpsClient()
	if err != nil {
		log.Fatalf("Failed to create Azure DevOps client: %v", err)
	}

	// Create MCP server
	s := server.NewMCPServer(
		"Azure DevOps MCP Server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithToolCapabilities(true),
	)

	// Add search tool
	searchTool := mcp.NewTool("search",
		mcp.WithDescription("Search for files in Azure DevOps repositories"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query"),
		),
		mcp.WithString("repo",
			mcp.Description("Optional repository name to search in"),
		),
	)

	s.AddTool(searchTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, ok := request.Params.Arguments["query"].(string)
		if !ok {
			log.Print("Query must be a string")
			return nil, fmt.Errorf("query must be a string")
		}

		repoName, _ := request.Params.Arguments["repo"].(string)

		results, err := client.searchRepository(ctx, query, repoName)
		if err != nil {
			log.Printf("Error searching repositories: %v", err)
			return nil, fmt.Errorf("error searching repositories: %w", err)
		}

		jsonData, err := json.Marshal(results)
		if err != nil {
			log.Printf("Error marshaling results: %v", err)
			return nil, fmt.Errorf("error marshaling results: %w", err)
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	})

	// Add read tool
	readTool := mcp.NewTool("read",
		mcp.WithDescription("Read file content from Azure DevOps"),
		mcp.WithString("repository",
			mcp.Required(),
			mcp.Description("Repository name"),
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("File path"),
		),
	)

	s.AddTool(readTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo, ok := request.Params.Arguments["repository"].(string)
		if !ok {
			log.Print("Repository must be a string")
			return nil, fmt.Errorf("repository must be a string")
		}

		path, ok := request.Params.Arguments["path"].(string)
		if !ok {
			log.Print("Path must be a string")
			return nil, fmt.Errorf("path must be a string")
		}

		content, err := client.getFileContent(ctx, repo, path)
		if err != nil {
			log.Printf("Error getting file content: %v", err)
			return nil, fmt.Errorf("error getting file content: %w", err)
		}

		return mcp.NewToolResultText(content), nil
	})

	// Create SSE server
	sseServer := server.NewSSEServer(s,
		server.WithBaseURL(fmt.Sprintf("http://%s:%d", client.config.Server.Host, client.config.Server.Port)),
	)

	// Start the SSE server
	log.Printf("SSE server listening on %s:%d", client.config.Server.Host, client.config.Server.Port)
	if err := sseServer.Start(fmt.Sprintf("%s:%d", client.config.Server.Host, client.config.Server.Port)); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
