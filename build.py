#!/usr/bin/env python3
"""
Azure ACR and Docker Automation Script

This script automates the end-to-end workflow for building a local Docker image,
authenticating with Azure Container Registry (ACR), tagging and pushing the image,
and optionally deploying the container instance on Microsoft Azure.
"""

import argparse
import os
import platform
import shutil
import sys
import subprocess
from typing import Dict, List, Optional


# Installation hints shown when a required dependency is missing on macOS.
_MACOS_INSTALL_HINTS: Dict[str, str] = {
    "docker": "brew install --cask docker  # or install Docker Desktop from https://www.docker.com",
    "az": "brew install azure-cli",
}


class DependencyChecker:
    """Verifies that required CLI tools are available on the system PATH before execution."""

    def __init__(self, required: List[str]):
        self.required = required

    def check(self) -> None:
        """Checks all required executables. Raises EnvironmentError if any are missing."""
        missing: List[str] = [cmd for cmd in self.required if shutil.which(cmd) is None]

        if not missing:
            return

        is_macos = platform.system() == "Darwin"
        lines = ["[ERROR] The following required tools were not found on your PATH:"]

        for cmd in missing:
            hint = _MACOS_INSTALL_HINTS.get(cmd, "Please install it manually and ensure it is on your PATH.")
            if is_macos:
                lines.append(f"  - {cmd}  →  {hint}")
            else:
                lines.append(f"  - {cmd}  →  Please install it and ensure it is on your PATH.")

        raise EnvironmentError("\n".join(lines))


class CommandRunner:
    """Handles execution of system commands with logging and dry-run support."""

    def __init__(self, dry_run: bool = False):
        self.dry_run = dry_run

    def _resolve_command(self, command: List[str]) -> List[str]:
        """Resolves the executable path (e.g. az -> az.cmd on Windows)."""
        if not command:
            return command

        executable = shutil.which(command[0])
        if executable is None:
            raise FileNotFoundError(command[0])

        if executable != command[0]:
            return [executable, *command[1:]]

        return command

    def run(self, command: List[str], check: bool = True) -> int:
        """Executes a system command or logs it if in dry-run mode."""
        formatted_cmd = " ".join(command)
        print(f"[INFO] Executing: {formatted_cmd}")

        if self.dry_run:
            print("[DRY-RUN] Command skipped.")
            return 0

        try:
            resolved_command = self._resolve_command(command)
            result = subprocess.run(resolved_command, check=check)
            return result.returncode
        except subprocess.CalledProcessError as err:
            print(f"[ERROR] Command failed with exit code {err.returncode}: {formatted_cmd}", file=sys.stderr)
            if check:
                raise
            return err.returncode
        except FileNotFoundError:
            print(f"[ERROR] Executable not found for command: {command[0]}", file=sys.stderr)
            if check:
                raise
            return 127


class AcrPublisher:
    """Manages Docker build, ACR login, tagging, and pushing operations."""

    def __init__(self, runner: CommandRunner, registry: str, image: str, tag: str, dockerfile: str):
        self.runner = runner
        self.registry = registry
        self.image = image
        self.tag = tag
        self.dockerfile = dockerfile

    @property
    def full_registry_host(self) -> str:
        return f"{self.registry}.azurecr.io"

    @property
    def local_image_name(self) -> str:
        return f"{self.image}:{self.tag}"

    @property
    def remote_image_name(self) -> str:
        return f"{self.full_registry_host}/{self.image}:{self.tag}"

    def build_image(self) -> None:
        """Builds the local Docker image using the specified Dockerfile."""
        print(f"[STAGE] Building local Docker image: {self.local_image_name}")
        cmd = ["docker", "build", "-f", self.dockerfile, "-t", self.local_image_name, "."]
        self.runner.run(cmd)

    def login_acr(self) -> None:
        """Authenticates Azure CLI with the specified Container Registry."""
        print(f"[STAGE] Authenticating with Azure Container Registry: {self.registry}")
        cmd = ["az", "acr", "login", "--name", self.registry]
        self.runner.run(cmd)

    def tag_image(self) -> None:
        """Tags local Docker image for remote ACR repository."""
        print(f"[STAGE] Tagging image {self.local_image_name} -> {self.remote_image_name}")
        cmd = ["docker", "tag", self.local_image_name, self.remote_image_name]
        self.runner.run(cmd)

    def push_image(self) -> None:
        """Pushes tagged image to Azure Container Registry."""
        print(f"[STAGE] Pushing image to ACR: {self.remote_image_name}")
        cmd = ["docker", "push", self.remote_image_name]
        self.runner.run(cmd)

    def execute_pipeline(self) -> None:
        """Executes full build, login, tag, and push sequence."""
        self.build_image()
        self.login_acr()
        self.tag_image()
        self.push_image()


class AciDeployer:
    """Manages automated deployment of container instances to Azure Container Instances (ACI)."""

    def __init__(self, runner: CommandRunner, resource_group: str, container_name: str, image_uri: str, port: int = 8080):
        self.runner = runner
        self.resource_group = resource_group
        self.container_name = container_name
        self.image_uri = image_uri
        self.port = port

    def deploy(self) -> None:
        """Deploys the image as an Azure Container Instance."""
        print(f"[STAGE] Deploying container instance '{self.container_name}' in resource group '{self.resource_group}'...")
        cmd = [
            "az", "container", "create",
            "--resource-group", self.resource_group,
            "--name", self.container_name,
            "--image", self.image_uri,
            "--dns-name-label", self.container_name,
            "--ports", str(self.port),
            "--ip-address", "Public"
        ]
        self.runner.run(cmd)


def parse_args(args: Optional[List[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Automate Docker build, ACR publish, and Azure deployment."
    )
    parser.add_argument(
        "--registry",
        default=os.getenv("ACR_NAME", "acringenieriaumjj"),
        help="Azure Container Registry name (default: acringenieriaumjj)"
    )
    parser.add_argument(
        "--image",
        default=os.getenv("IMAGE_NAME", "app-azure"),
        help="Docker image repository name (default: app-azure)"
    )
    parser.add_argument(
        "--tag",
        default=os.getenv("IMAGE_TAG", "v1.0.0"),
        help="Docker image tag (default: v1.0.0)"
    )
    parser.add_argument(
        "--dockerfile",
        default=os.getenv("DOCKERFILE_PATH", "Dockerfile"),
        help="Path to Dockerfile (default: Dockerfile)"
    )
    parser.add_argument(
        "--resource-group",
        default=os.getenv("AZURE_RESOURCE_GROUP", "azr-ingenieria-um-bujaldon"),
        help="Azure Resource Group name (default: azr-ingenieria-um-bujaldon)"
    )
    parser.add_argument(
        "--deploy",
        action="store_true",
        help="Deploy container instance to ACI after pushing image"
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print commands without executing them"
    )
    return parser.parse_args(args)


def main(cli_args: Optional[List[str]] = None) -> None:
    args = parse_args(cli_args)

    try:
        DependencyChecker(required=["docker", "az"]).check()
    except EnvironmentError as dep_err:
        print(dep_err, file=sys.stderr)
        sys.exit(1)

    runner = CommandRunner(dry_run=args.dry_run)

    publisher = AcrPublisher(
        runner=runner,
        registry=args.registry,
        image=args.image,
        tag=args.tag,
        dockerfile=args.dockerfile
    )

    try:
        publisher.execute_pipeline()

        if args.deploy:
            deployer = AciDeployer(
                runner=runner,
                resource_group=args.resource_group,
                container_name=args.image,
                image_uri=publisher.remote_image_name
            )
            deployer.deploy()

        print("[SUCCESS] All automated tasks completed successfully.")
    except Exception as err:
        print(f"[FATAL] Pipeline execution failed: {err}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
