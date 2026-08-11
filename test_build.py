#!/usr/bin/env python3
"""
Unit tests for build.py Azure ACR and Docker automation script.
Adheres strictly to AAA pattern and descriptive assertion naming conventions.
"""

import unittest
from unittest.mock import MagicMock, patch
import subprocess

from build import CommandRunner, AcrPublisher, AciDeployer, parse_args


class TestCommandRunner(unittest.TestCase):
    def test_should_skipExecution_when_dryRunModeIsEnabled(self):
        # Arrange
        runner = CommandRunner(dry_run=True)

        # Act
        return_code = runner.run(["echo", "hello"])

        # Assert
        self.assertEqual(return_code, 0)

    @patch("subprocess.run")
    def test_should_executeSubprocess_when_dryRunIsDisabled(self, mock_subprocess_run):
        # Arrange
        mock_subprocess_run.return_value = MagicMock(returncode=0)
        runner = CommandRunner(dry_run=False)

        # Act
        return_code = runner.run(["docker", "build", "-t", "app:v1", "."])

        # Assert
        self.assertEqual(return_code, 0)
        mock_subprocess_run.assert_called_once_with(["docker", "build", "-t", "app:v1", "."], check=True)

    @patch("subprocess.run")
    def test_should_raiseProcessError_when_commandFailsAndCheckIsTrue(self, mock_subprocess_run):
        # Arrange
        mock_subprocess_run.side_effect = subprocess.CalledProcessError(1, ["docker", "push"])
        runner = CommandRunner(dry_run=False)

        # Act & Assert
        with self.assertRaises(subprocess.CalledProcessError):
            runner.run(["docker", "push"], check=True)


class TestAcrPublisher(unittest.TestCase):
    def setUp(self):
        self.runner = MagicMock(spec=CommandRunner)
        self.publisher = AcrPublisher(
            runner=self.runner,
            registry="acringenieriaumjj",
            image="app-azure",
            tag="v1.0.0",
            dockerfile="Dockerfile"
        )

    def test_should_formatRegistryAndImageURIsCorrectly(self):
        # Assert
        self.assertEqual(self.publisher.full_registry_host, "acringenieriaumjj.azurecr.io")
        self.assertEqual(self.publisher.local_image_name, "app-azure:v1.0.0")
        self.assertEqual(self.publisher.remote_image_name, "acringenieriaumjj.azurecr.io/app-azure:v1.0.0")

    def test_should_issueDockerBuildCommand_when_buildImageCalled(self):
        # Act
        self.publisher.build_image()

        # Assert
        self.runner.run.assert_called_once_with(["docker", "build", "-f", "Dockerfile", "-t", "app-azure:v1.0.0", "."])

    def test_should_issueAzAcrLoginCommand_when_loginAcrCalled(self):
        # Act
        self.publisher.login_acr()

        # Assert
        self.runner.run.assert_called_once_with(["az", "acr", "login", "--name", "acringenieriaumjj"])

    def test_should_issueDockerTagCommand_when_tagImageCalled(self):
        # Act
        self.publisher.tag_image()

        # Assert
        self.runner.run.assert_called_once_with(
            ["docker", "tag", "app-azure:v1.0.0", "acringenieriaumjj.azurecr.io/app-azure:v1.0.0"]
        )

    def test_should_issueDockerPushCommand_when_pushImageCalled(self):
        # Act
        self.publisher.push_image()

        # Assert
        self.runner.run.assert_called_once_with(
            ["docker", "push", "acringenieriaumjj.azurecr.io/app-azure:v1.0.0"]
        )

    def test_should_executeAllPipelineStepsSequentially(self):
        # Act
        self.publisher.execute_pipeline()

        # Assert
        self.assertEqual(self.runner.run.call_count, 4)


class TestAciDeployer(unittest.TestCase):
    def test_should_issueAzContainerCreateCommand_when_deployCalled(self):
        # Arrange
        runner = MagicMock(spec=CommandRunner)
        deployer = AciDeployer(
            runner=runner,
            resource_group="rg-test",
            container_name="app-azure",
            image_uri="acringenieriaumjj.azurecr.io/app-azure:v1.0.0",
            port=8080
        )

        # Act
        deployer.deploy()

        # Assert
        runner.run.assert_called_once_with([
            "az", "container", "create",
            "--resource-group", "rg-test",
            "--name", "app-azure",
            "--image", "acringenieriaumjj.azurecr.io/app-azure:v1.0.0",
            "--dns-name-label", "app-azure",
            "--ports", "8080",
            "--ip-address", "Public"
        ])


class TestArgumentParser(unittest.TestCase):
    def test_should_assignDefaultArguments_when_noCliArgsProvided(self):
        # Act
        args = parse_args([])

        # Assert
        self.assertEqual(args.registry, "acringenieriaumjj")
        self.assertEqual(args.image, "app-azure")
        self.assertEqual(args.tag, "v1.0.0")
        self.assertEqual(args.dockerfile, "Dockerfile")
        self.assertEqual(args.resource_group, "rg-ingenieria-um")
        self.assertFalse(args.deploy)
        self.assertFalse(args.dry_run)

    def test_should_overrideDefaults_when_customCliArgsProvided(self):
        # Act
        args = parse_args([
            "--registry", "myacr",
            "--image", "myimage",
            "--tag", "v2.0.0",
            "--deploy",
            "--dry-run"
        ])

        # Assert
        self.assertEqual(args.registry, "myacr")
        self.assertEqual(args.image, "myimage")
        self.assertEqual(args.tag, "v2.0.0")
        self.assertTrue(args.deploy)
        self.assertTrue(args.dry_run)


if __name__ == "__main__":
    unittest.main()
