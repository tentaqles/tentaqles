"""CLI entry point for the workspace-aware MCP server."""

import argparse

from sidecar.mcp_server import server


def main():
    parser = argparse.ArgumentParser(description="Workspace-aware MCP server")
    parser.add_argument(
        "--workspace",
        required=True,
        help="Path to the workspace directory (e.g., D:/repos/booster)",
    )
    args = parser.parse_args()

    server._workspace_path = args.workspace

    mcp = server.create_server()
    mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
