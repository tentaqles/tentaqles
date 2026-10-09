"""Gated build orchestration (/tentaqles:build) and the overnight loop (/tentaqles:loop).

Everything here is standard-library only so the hook entry points start fast and
never trigger the plugin's dependency bootstrap.
"""
