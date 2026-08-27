"""Tentaqles native graph engine — built-in knowledge graph generation.

This is a self-contained graph engine that does not depend on the external
graphify package. It includes all the enhancements previously patched onto
graphify: .gitignore support, docling integration, offline HTML, semantic
search, and memory integration.

Module map (build incrementally):
  detect.py    — file scanning with cascading .gitignore/.graphifyignore
  extract.py   — AST extraction (tree-sitter) + semantic extraction (Claude)
  build.py     — networkx graph construction from extraction results
  cluster.py   — community detection (Louvain/Leiden)
  analyze.py   — god nodes, surprising connections, suggested questions
  report.py    — GRAPH_REPORT.md generation
  export.py    — HTML (offline), JSON, Obsidian, SVG, Neo4j, GraphML
  serve.py     — MCP server with semantic search
  cache.py     — extraction caching (SHA256-based)
"""
