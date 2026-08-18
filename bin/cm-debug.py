#!/usr/bin/env python3
"""cm-debug: CodeMender Environment Diagnostic and Setup Verification Tool.

Standalone interactive script to verify that Google Cloud SDK, Application
Default Credentials (ADC), quota project settings, required APIs, and Vertex AI
IAM permissions are correctly configured for CodeMender.
"""

from __future__ import annotations

from collections.abc import Sequence
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from typing import Any, NamedTuple
import urllib.error
import urllib.request


REQUIRED_CODEMENDER_PERMISSIONS: tuple[str, ...] = (
    "aiplatform.endpoints.predict",
    "aiplatform.sessions.create",
    "aiplatform.sessions.get",
    "aiplatform.sessions.list",
    "aiplatform.sessions.update",
    "aiplatform.sessions.delete",
    "aiplatform.sessionEvents.append",
    "aiplatform.sessionEvents.list",
)


class CommandResult(NamedTuple):
    """Result of an executed subprocess command."""

    returncode: int
    stdout: str
    stderr: str

    @property
    def success(self) -> bool:
        """Returns True if the command exited with code 0."""
        return self.returncode == 0


def run_cmd(
    cmd: Sequence[str],
    timeout_sec: int = 15,
) -> CommandResult:
    """Executes a command safely with an outer timeout.

    Args:
        cmd: The command line arguments to execute.
        timeout_sec: Maximum execution time in seconds before terminating.

    Returns:
        A CommandResult containing returncode, stdout, and stderr.
    """
    if not cmd:
        return CommandResult(-2, "", "Empty command provided")
    try:
        proc = subprocess.run(
            list(cmd),
            capture_output=True,
            text=True,
            timeout=timeout_sec,
            shell=False,
        )
        return CommandResult(
            proc.returncode,
            proc.stdout.strip(),
            proc.stderr.strip(),
        )
    except subprocess.TimeoutExpired:
        return CommandResult(
            -1,
            "",
            f"Command timed out after {timeout_sec}s: {' '.join(cmd)}",
        )
    except FileNotFoundError:
        return CommandResult(-2, "", f"Executable not found: {cmd[0]}")
    except OSError as err:
        return CommandResult(-3, "", str(err))


def get_adc_file_path() -> Path:
    """Resolves the Application Default Credentials file path cross-platform.

    Source of truth:
    - Google Cloud ADC Specification:
      https://cloud.google.com/docs/authentication/application-default-credentials#personal
    - gcloud ADC reference:
      https://cloud.google.com/sdk/gcloud/reference/auth/application-default/login
    - Python reference implementation:
      https://github.com/googleapis/google-auth-library-python/blob/main/google/auth/_default.py
    - Go reference implementation:
      https://github.com/golang/oauth2/blob/master/google/find_default_credentials.go

    Returns:
        Path to the resolved ADC JSON file.
    """
    env_creds = os.environ.get("GOOGLE_APPLICATION_CREDENTIALS")
    if env_creds:
        return Path(env_creds).expanduser()

    cloudsdk_config = os.environ.get("CLOUDSDK_CONFIG")
    if cloudsdk_config:
        return (
            Path(cloudsdk_config).expanduser()
            / "application_default_credentials.json"
        )

    if sys.platform == "win32":
        appdata = os.environ.get("APPDATA")
        if appdata:
            return (
                Path(appdata) / "gcloud" / "application_default_credentials.json"
            )
        return (
            Path.home()
            / "AppData"
            / "Roaming"
            / "gcloud"
            / "application_default_credentials.json"
        )

    return (
        Path.home()
        / ".config"
        / "gcloud"
        / "application_default_credentials.json"
    )


def read_adc_json() -> dict[str, Any] | None:
    """Reads and parses the ADC JSON file if it exists.

    Returns:
        Parsed JSON dictionary if file exists and is valid JSON, else None.
    """
    adc_path = get_adc_file_path()
    if not adc_path.is_file():
        return None
    try:
        with adc_path.open("r", encoding="utf-8") as f:
            data = json.load(f)
            if isinstance(data, dict):
                return data
    except (OSError, json.JSONDecodeError, UnicodeDecodeError):
        return None
    return None


def print_pass(msg: str) -> None:
    """Prints a passing check result."""
    print(f"  [PASS] {msg}")


def print_fail(msg: str) -> None:
    """Prints a failing check result."""
    print(f"  [FAIL] {msg}")


def print_info(msg: str) -> None:
    """Prints an informational diagnostic note."""
    print(f"  [INFO] {msg}")


def print_remediation(instruction: str, command: str | None = None) -> None:
    """Prints actionable remediation steps."""
    print(f"\n  Remediation: {instruction}")
    if command:
        print(f"  $ {command}\n")
    else:
        print()


def check_cm_installed() -> bool:
    """Checks whether the CodeMender CLI (cm) binary is present and executable.

    Source of truth:
    - https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment

    Returns:
        True if cm binary is installed and executable, False otherwise.
    """
    print("\n[1/8] Checking CodeMender CLI (cm) binary...")
    cm_bin = shutil.which("cm")
    if not cm_bin:
        print_fail(
            "CodeMender CLI binary ('cm') is not installed or not in PATH."
        )
        print_remediation(
            "Install CodeMender CLI following the official setup guide:",
            "https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment",
        )
        return False

    code, out, _ = run_cmd(["cm", "--version"], timeout_sec=10)
    version = out.splitlines()[0] if code == 0 and out else "available"
    print_pass(f"Found cm at {cm_bin} ({version})")
    return True


def verify_cm_initialized() -> bool:
    """Verifies that CodeMender has been initialized via 'cm init --verify'.

    Source of truth:
    - https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment

    Returns:
        True if cm initialization verified successfully, False otherwise.
    """
    print("\n[2/8] Verifying CodeMender initialization (cm init --verify)...")
    code, out, err = run_cmd(["cm", "init", "--verify"], timeout_sec=30)
    combined = f"{out}\n{err}".strip()

    has_failure = code != 0
    failure_details: list[str] = []

    # Parse stdout and stderr lines for Results line and failure indicators
    for line in combined.splitlines():
        line_clean = line.strip()
        lower_line = line_clean.lower()

        # Check Results line (e.g. "Results: 3 passed, 1 failure, 0 warnings")
        if "results:" in lower_line or "result:" in lower_line:
            fail_match = re.search(
                r"(\d+)\s+(?:failure|failed|error)", lower_line
            )
            if fail_match:
                if int(fail_match.group(1)) > 0:
                    has_failure = True
                    failure_details.append(line_clean)
            elif "fail" in lower_line and not re.search(
                r"\b0\s+(?:failure|failed|error)s?\b", lower_line
            ):
                has_failure = True
                failure_details.append(line_clean)

        # Check individual check failures
        elif (
            line_clean.startswith("[FAIL]")
            or line_clean.startswith("FAIL:")
            or line_clean.startswith("FAILED:")
            or line_clean.startswith("[ERROR]")
            or line_clean.startswith("ERROR:")
        ):
            has_failure = True
            failure_details.append(line_clean)

    if has_failure:
        print_fail(
            "CodeMender initialization verification ('cm init --verify')"
            " reported failures."
        )
        if failure_details:
            for detail in failure_details:
                print_info(detail)
        elif combined:
            for detail_line in combined.splitlines():
                if detail_line.strip():
                    print_info(detail_line.strip())
        print_remediation(
            "Initialize CodeMender configuration for your workspace:",
            "cm init",
        )
        return False

    print_pass("CodeMender initialization verified ('cm init --verify' passed).")
    return True


def check_gcloud() -> bool:
    """Checks whether the Google Cloud CLI (gcloud) is installed.

    Source of truth:
    - https://cloud.google.com/sdk/docs/install

    Returns:
        True if gcloud is installed and executable, False otherwise.
    """
    print("\n[3/8] Checking gcloud installation...")
    gcloud_bin = shutil.which("gcloud")
    if not gcloud_bin:
        print_fail(
            "Google Cloud SDK ('gcloud') is not installed or not in PATH."
        )
        print_remediation(
            "Please install the Google Cloud SDK:",
            "https://cloud.google.com/sdk/docs/install",
        )
        return False

    code, out, _ = run_cmd(["gcloud", "--version"], timeout_sec=10)
    version = out.splitlines()[0] if code == 0 and out else "available"
    print_pass(f"Found gcloud at {gcloud_bin} ({version})")
    return True


def get_adc_account() -> str | None:
    """Retrieves the active user account associated with ADC or gcloud.

    Returns:
        Account email string if found, else None.
    """
    adc_data = read_adc_json()
    if adc_data and adc_data.get("account"):
        return str(adc_data.get("account")).strip()

    code, out, _ = run_cmd(
        ["gcloud", "config", "get-value", "account", "--quiet"], timeout_sec=5
    )
    if code == 0 and out:
        lines = [
            line.strip()
            for line in out.splitlines()
            if line.strip() and not line.startswith("Your active")
        ]
        if lines and lines[0] != "(unset)":
            return lines[0]

    code, out, _ = run_cmd(
        [
            "gcloud",
            "auth",
            "list",
            "--filter=status:ACTIVE",
            "--format=value(account)",
            "--quiet",
        ],
        timeout_sec=5,
    )
    if code == 0 and out.strip():
        return out.strip().splitlines()[0]

    return None


def verify_adc_active() -> bool:
    """Verifies that Application Default Credentials (ADC) are active and valid.

    Source of truth:
    - https://cloud.google.com/docs/authentication/application-default-credentials
    - https://cloud.google.com/sdk/gcloud/reference/auth/application-default/print-access-token

    Returns:
        True if ADC is valid and confirmed, False otherwise.
    """
    print("\n[4/8] Verifying Application Default Credentials (ADC)...")

    adc_path = get_adc_file_path()
    if not adc_path.exists():
        print_fail(f"ADC credentials file not found at: {adc_path}")
        print_remediation(
            "Acquire Application Default Credentials via gcloud:",
            "gcloud auth application-default login",
        )
        return False

    code, out, _ = run_cmd(
        [
            "gcloud",
            "auth",
            "application-default",
            "print-access-token",
            "--quiet",
        ],
        timeout_sec=15,
    )

    if code != 0 or not out.strip():
        print_fail(
            "Application Default Credentials (ADC) are inactive, expired,"
            " or require login."
        )
        print_remediation(
            "Log in to refresh your Application Default Credentials:",
            "gcloud auth application-default login",
        )
        return False

    account = get_adc_account()
    if account:
        print_info(f"Detected ADC Account: {account}")
        try:
            user_response = input(
                f"\n  ? Is '{account}' the correct Google Cloud user account"
                " for CodeMender? [y/N]: "
            ).strip().lower()
        except (KeyboardInterrupt, EOFError):
            print()
            print_fail("Verification aborted by user.")
            return False

        if user_response not in ("y", "yes"):
            print_fail(f"Account '{account}' was marked as incorrect for CodeMender.")
            print_remediation(
                "Log in with the correct Google Cloud account:",
                "gcloud auth application-default login",
            )
            return False

        print_pass(f"Confirmed active ADC account: {account}")
    else:
        print_pass("Application Default Credentials are active and valid.")

    return True


def get_project_number(project_id: str) -> str | None:
    """Retrieves the numeric project number for a given project ID if accessible.

    Source of truth:
    - https://cloud.google.com/sdk/gcloud/reference/projects/describe

    Args:
        project_id: Google Cloud project ID string.

    Returns:
        Numeric project number string if resolved, else None.
    """
    code, out, _ = run_cmd(
        [
            "gcloud",
            "projects",
            "describe",
            project_id,
            "--format=value(projectNumber)",
            "--quiet",
        ],
        timeout_sec=10,
    )
    if code == 0 and out.strip():
        return out.strip()
    return None


def verify_adc_quota_project() -> str | None:
    """Verifies that an ADC quota project is configured and confirmed.

    Source of truth:
    - https://cloud.google.com/docs/authentication/adc-troubleshooting/user-creds#quota-project
    - https://cloud.google.com/sdk/gcloud/reference/auth/application-default/set-quota-project

    Returns:
        Confirmed quota project ID string if verified, else None.
    """
    print("\n[5/8] Verifying ADC Quota Project ID & Allowlist status...")
    adc_data = read_adc_json()
    quota_project = None
    if adc_data and adc_data.get("quota_project_id"):
        quota_project = str(adc_data.get("quota_project_id")).strip()

    if not quota_project:
        print_fail(
            "No 'quota_project_id' is set in Application Default Credentials."
        )
        print_remediation(
            "Set your ADC quota project to your allowlisted CodeMender project"
            " ID:\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageConsumer' on project"
            " '<PROJECT_ID>')",
            "gcloud auth application-default set-quota-project <PROJECT_ID>",
        )
        return None

    project_num = get_project_number(quota_project)
    num_str = f" (Project Number: {project_num})" if project_num else ""
    print_info(f"Detected ADC Quota Project ID: {quota_project}{num_str}")

    try:
        user_response = input(
            f"\n  ? Is the project ID '{quota_project}'{num_str} allowlisted"
            " for CodeMender? [y/N]: "
        ).strip().lower()
    except (KeyboardInterrupt, EOFError):
        print()
        print_fail("Verification aborted by user.")
        return None

    if user_response not in ("y", "yes"):
        print_fail(
            f"Project ID '{quota_project}' is not allowlisted for CodeMender."
        )
        print_remediation(
            "Configure ADC with your allowlisted CodeMender project ID:\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageConsumer' on project"
            " '<ALLOWLISTED_PROJECT_ID>')",
            "gcloud auth application-default set-quota-project"
            " <ALLOWLISTED_PROJECT_ID>",
        )
        return None

    print_pass(f"Allowlisted project ID confirmed: {quota_project}{num_str}")
    return quota_project


def verify_gcloud_config_project(quota_project: str) -> bool:
    """Verifies that the active gcloud CLI project matches the ADC quota project.

    Source of truth:
    - https://cloud.google.com/sdk/gcloud/reference/config/set

    Args:
        quota_project: The expected ADC quota project ID.

    Returns:
        True if gcloud config project matches quota project, False otherwise.
    """
    print("\n[6/8] Verifying gcloud config project matches ADC quota project...")
    code, out, _ = run_cmd(
        ["gcloud", "config", "get-value", "project", "--quiet"], timeout_sec=10
    )

    gcloud_project = None
    if code == 0 and out:
        lines = [
            line.strip()
            for line in out.splitlines()
            if line.strip() and not line.startswith("Your active")
        ]
        if lines and lines[0] != "(unset)":
            gcloud_project = lines[0]

    if not gcloud_project:
        print_fail(
            "No project is set in gcloud config (expected ADC quota project"
            f" '{quota_project}')."
        )
        print_remediation(
            f"Set active gcloud project to '{quota_project}':\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageConsumer' on project"
            f" '{quota_project}')",
            f"gcloud config set project {quota_project}",
        )
        return False

    if gcloud_project != quota_project:
        print_fail(
            f"gcloud config project ('{gcloud_project}') does not match ADC"
            f" quota project ('{quota_project}')."
        )
        print_remediation(
            "Update gcloud configuration to match ADC quota project ID"
            f" '{quota_project}':\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageConsumer' on project"
            f" '{quota_project}')",
            f"gcloud config set project {quota_project}",
        )
        return False

    print_pass(
        f"gcloud config project matches ADC quota project: {quota_project}"
    )
    return True


def verify_apis_active(quota_project: str) -> bool:
    """Verifies that Vertex AI and Cloud Resource Manager APIs are enabled.

    Source of truth:
    - https://cloud.google.com/service-usage/docs/enable-disable
    - https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment

    Args:
        quota_project: Google Cloud project ID string.

    Returns:
        True if all required APIs are enabled, False otherwise.
    """
    print("\n[7/8] Verifying required Google Cloud APIs are active on project...")

    required_apis = (
        "aiplatform.googleapis.com",
        "cloudresourcemanager.googleapis.com",
    )

    code, out, _ = run_cmd(
        [
            "gcloud",
            "services",
            "list",
            "--enabled",
            f"--project={quota_project}",
            (
                "--filter=config.name:(aiplatform.googleapis.com OR"
                " cloudresourcemanager.googleapis.com)"
            ),
            "--format=value(config.name)",
            "--quiet",
        ],
        timeout_sec=15,
    )

    if code != 0:
        print_fail(
            f"Failed to query enabled services on project '{quota_project}'."
        )
        print_remediation(
            f"Query enabled services on project '{quota_project}':\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageConsumer' on project"
            f" '{quota_project}')",
            f"gcloud services list --enabled --project={quota_project}",
        )
        return False

    enabled_services = set(out.splitlines()) if out else set()
    missing = [api for api in required_apis if api not in enabled_services]

    for api in required_apis:
        if api in enabled_services:
            print(f"    - {api}: Enabled")
        else:
            print(f"    - {api}: NOT ENABLED")

    if missing:
        print_fail(
            f"Missing required APIs on project '{quota_project}':"
            f" {', '.join(missing)}"
        )
        print_remediation(
            f"Enable missing APIs on project '{quota_project}':\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageAdmin' on project"
            f" '{quota_project}')",
            f"gcloud services enable {' '.join(missing)}"
            f" --project={quota_project}",
        )
        return False

    print_pass(f"All required APIs are active on project '{quota_project}'.")
    return True


def test_iam_permissions(
    project_id: str,
    permissions: Sequence[str],
) -> tuple[bool, list[str], str | None]:
    """Tests project-level IAM permissions using testIamPermissions API with ADC.

    Source of truth:
    - https://docs.cloud.google.com/iam/docs/testing-permissions
    - https://cloud.google.com/resource-manager/reference/rest/v1/projects/testIamPermissions

    Args:
        project_id: Google Cloud project ID string.
        permissions: Sequence of permission string names to test.

    Returns:
        A tuple of (success, held_permissions_list, error_message_or_none).
    """
    token_code, token, token_err = run_cmd(
        [
            "gcloud",
            "auth",
            "application-default",
            "print-access-token",
            "--quiet",
        ],
        timeout_sec=10,
    )
    if token_code != 0 or not token.strip():
        err_msg = (
            token_err
            if token_err
            else "Failed to acquire Application Default Credentials access token"
        )
        return False, [], err_msg

    url = (
        "https://cloudresourcemanager.googleapis.com/v1/projects/"
        f"{project_id}:testIamPermissions"
    )
    payload = json.dumps({"permissions": list(permissions)}).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=payload,
        headers={
            "Authorization": f"Bearer {token.strip()}",
            "Content-Type": "application/json; charset=utf-8",
        },
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            held_perms = data.get("permissions", [])
            return True, held_perms, None
    except urllib.error.HTTPError as e:
        error_body = e.read().decode("utf-8", errors="replace").strip()
        try:
            error_json = json.loads(error_body)
            api_message = error_json.get("error", {}).get("message")
            if api_message:
                return False, [], f"HTTP {e.code}: {api_message}"
        except (json.JSONDecodeError, AttributeError):
            pass
        return False, [], f"HTTP {e.code}: {e.reason}"
    except urllib.error.URLError as e:
        return False, [], f"Network error: {e.reason}"
    except (OSError, json.JSONDecodeError) as e:
        return False, [], str(e)


def verify_user_permissions(quota_project: str) -> bool:
    """Verifies that the user has required IAM roles/permissions for CodeMender.

    Source of truth:
    - CodeMender environment requirements (roles/aiplatform.user):
      https://docs.cloud.google.com/gemini-enterprise-agent-platform/codemender/set-up-environment
    - Vertex AI Access Control (IAM roles and permissions):
      https://cloud.google.com/vertex-ai/docs/general/access-control
    - Cloud Resource Manager testIamPermissions:
      https://docs.cloud.google.com/iam/docs/testing-permissions

    Args:
        quota_project: Google Cloud project ID string.

    Returns:
        True if all required permissions are held, False otherwise.
    """
    print("\n[8/8] Verifying user permissions on project...")

    target_role = "roles/aiplatform.user"

    # Get active account email
    account = get_adc_account()
    account_label = account if account else "Current user"

    print_info(
        "Testing direct Vertex AI IAM permissions on project"
        f" '{quota_project}'..."
    )
    success, held_perms, err = test_iam_permissions(
        quota_project, REQUIRED_CODEMENDER_PERMISSIONS
    )

    if not success:
        print_fail(
            f"Failed to test permissions on project '{quota_project}': {err}"
        )
        member_binding = f"user:{account}" if account else "user:<YOUR_EMAIL>"
        print_remediation(
            f"Grant '{target_role}' to {account_label} on project"
            f" '{quota_project}':\n"
            "  (Ensure you have the IAM role"
            " 'roles/serviceusage.serviceUsageConsumer' on project"
            f" '{quota_project}')",
            f"gcloud projects add-iam-policy-binding {quota_project}"
            f' --member="{member_binding}" --role="{target_role}"',
        )
        return False

    missing = [
        p for p in REQUIRED_CODEMENDER_PERMISSIONS if p not in held_perms
    ]

    if missing:
        print_fail(
            f"User {account_label} is missing required Vertex AI permissions"
            f" on project '{quota_project}':"
        )
        for p in REQUIRED_CODEMENDER_PERMISSIONS:
            if p in held_perms:
                print(f"    - {p}: GRANTED")
            else:
                print(f"    - {p}: MISSING")

        member_binding = f"user:{account}" if account else "user:<YOUR_EMAIL>"
        print_remediation(
            f"Grant '{target_role}' to {account_label} on project"
            f" '{quota_project}':",
            f"gcloud projects add-iam-policy-binding {quota_project}"
            f' --member="{member_binding}" --role="{target_role}"',
        )
        return False

    print_pass(
        "Successfully verified all required Vertex AI permissions for"
        f" {account_label} on project '{quota_project}'."
    )
    return True


def print_environment_overview() -> None:
    """Prints the upfront environment setup requirements."""
    print("=" * 70)
    print(" CodeMender (cm) Environment Diagnostic Tool (cm-debug)")
    print("=" * 70)
    print("\nCodeMender requires the following to work:\n")
    print("1. Local Environment Config:")
    print("   - GCloud Application Default Credentials authenticated.")
    print("   - GCloud Config set to the correct project.")
    print(
        "   - GCloud Application Default Credentials quota project and"
        " gcloud config project set correctly.\n"
    )
    print("2. Required APIs enabled on the project:")
    print("   - aiplatform.googleapis.com (Vertex AI API)")
    print(
        "   - cloudresourcemanager.googleapis.com (Cloud Resource Manager"
        " API)\n"
    )
    print("3. Required IAM Roles on the project:")
    print(
        "   - roles/serviceusage.serviceUsageConsumer (to set the ADC"
        " quota project)"
    )
    print(
        "   - roles/aiplatform.user (Vertex AI User - required for AI"
        " operations)\n"
    )
    print("=" * 70)


def main() -> int:
    """Main entry point for cm-debug CLI.

    Returns:
        Exit code 0 on success, non-zero on failure.
    """
    print_environment_overview()

    if not check_cm_installed():
        return 1

    if not verify_cm_initialized():
        return 1

    if not check_gcloud():
        return 1

    if not verify_adc_active():
        return 1

    quota_project = verify_adc_quota_project()
    if not quota_project:
        return 1

    if not verify_gcloud_config_project(quota_project):
        return 1

    if not verify_apis_active(quota_project):
        return 1

    if not verify_user_permissions(quota_project):
        return 1

    print("\n" + "=" * 65)
    print(" RESULT: All environment checks PASSED!")
    print(f" Environment is ready for CodeMender on project '{quota_project}'.")
    print("=" * 65 + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
