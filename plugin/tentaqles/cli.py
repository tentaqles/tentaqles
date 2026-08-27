"""Tentaqles CLI — setup and management commands."""
import argparse
import sys


def main():
    parser = argparse.ArgumentParser(description="Tentaqles workspace orchestration")
    sub = parser.add_subparsers(dest="command")

    demo_p = sub.add_parser("demo", help="Create demo workspaces with sample code")
    demo_p.add_argument("path", nargs="?", default=".", help="Where to create the demo")

    init_p = sub.add_parser("init", help="Initialize Tentaqles in current workspace")

    status_p = sub.add_parser("status", help="Show workspace detection status")
    status_p.add_argument("path", nargs="?", default=".", help="Path to check")

    args = parser.parse_args()

    if args.command == "demo":
        from tentaqles.demo import create_demo
        result = create_demo(args.path)
        print(result)
    elif args.command == "status":
        from tentaqles.manifest.loader import get_client_context, run_preflight_checks, format_context_summary
        ctx = get_client_context(args.path)
        from tentaqles.manifest.loader import load_manifest
        checks = run_preflight_checks(load_manifest(args.path) or ctx)
        print(format_context_summary(ctx, checks))
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
