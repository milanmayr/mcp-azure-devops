# Azure DevOps MCP Server

This is a Cursor MCP server that integrates with Azure DevOps to read code across repositories in the HCC project. It uses the official [mcp-go](https://github.com/mark3labs/mcp-go) package for the MCP server implementation.

## Setup

1. Install Go 1.21 or later
2. Clone this repository
3. Install dependencies:
   ```bash
   go mod download
   ```

4. Create a Personal Access Token (PAT) in Azure DevOps:
   - Go to Azure DevOps > User Settings > Personal Access Tokens
   - Create a new token with the following scopes:
     - Code (Read)
   - Copy the generated token

5. Configure the server:
   - Edit `config.yaml` and fill in your organization details
   - Set your PAT either in `config.yaml` or via environment variable:
     ```bash
     export AZURE_DEVOPS_PAT=your_pat_here
     ```

## Running the Server

```bash
go run main.go
```

The server will start and listen for SSE connections on the configured host and port (default: localhost:8080).

## Available Tools

The server provides the following MCP tools:

### Search Tool
Search for files in Azure DevOps repositories.

Parameters:
- `query` (required): Search query string
- `repo` (optional): Repository name to search in

### Read Tool
Read file content from Azure DevOps.

Parameters:
- `repository` (required): Repository name
- `path` (required): File path
- `branch` (required): Branch name

## Configuration

The server can be configured through `config.yaml`:

```yaml
azure_devops:
  organization: "your-org"
  project: "HCC"
  pat: "" # Optional, can be set via AZURE_DEVOPS_PAT environment variable
  api_version: "6.0"

server:
  port: 8080
  host: "localhost"
```
