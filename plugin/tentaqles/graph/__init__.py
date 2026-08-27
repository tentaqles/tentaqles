"""Tentaqles graph engine — pluggable backend for knowledge graph generation."""

from .engine import get_engine, GraphEngine

__all__ = ["get_engine", "GraphEngine"]
