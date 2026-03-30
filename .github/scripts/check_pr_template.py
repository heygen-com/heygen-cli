import json
import os
import sys

import requests

MIN_DESCRIPTION_LENGTH = 50

# Load the GitHub event data
event_path = os.getenv("GITHUB_EVENT_PATH")
assert event_path
with open(event_path, "r") as f:
    event_data = json.load(f)

# Get the pull request number
pr_number = event_data["pull_request"]["number"]
repo = os.getenv("GITHUB_REPOSITORY")
token = os.getenv("GITHUB_TOKEN")

# GitHub API to get PR details
url = f"https://api.github.com/repos/{repo}/pulls/{pr_number}"

headers = {
    "Authorization": f"token {token}",
    "Accept": "application/vnd.github.v3+json",
}

response = requests.get(url, headers=headers)
pr_data = response.json()
pr_body = (pr_data.get("body") or "").strip()

if len(pr_body) < MIN_DESCRIPTION_LENGTH:
    print(f"ERROR: PR description must be at least {MIN_DESCRIPTION_LENGTH} characters (currently {len(pr_body)}).")
    sys.exit(1)

print(f"PR description is {len(pr_body)} characters. OK.")
sys.exit(0)
