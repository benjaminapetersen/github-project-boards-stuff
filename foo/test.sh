#!/bin/bash

# Test script to demonstrate the automation tool usage
# This script shows example usage without making actual API calls

echo "GitHub Project Board Automation - Test Script"
echo "=============================================="
echo ""

echo "1. Testing without environment variables (should fail):"
./github-board-automation 2>&1 | head -2
echo ""

echo "2. Testing with GITHUB_TOKEN but no username (should fail):"
export GITHUB_TOKEN="dummy_token_for_test"
./github-board-automation 2>&1 | head -2
echo ""

echo "3. To run with real credentials, set:"
echo "   export GITHUB_TOKEN='your_personal_access_token'"
echo "   export GITHUB_USERNAME='your_github_username'"
echo "   export PROJECT_ID='your_project_id' (optional)"
echo ""

echo "4. Example execution (with dummy values):"
echo "   GITHUB_TOKEN=ghp_xxxxx GITHUB_USERNAME=yourname ./github-board-automation"
echo ""

echo "The tool will:"
echo "  - Search for commits in Kubernetes org repos from the last month"
echo "  - Search for PRs you've authored in Kubernetes org"
echo "  - Search for issues you've created in Kubernetes org"
echo "  - Display a summary of found items"
echo "  - Optionally add them to a project board (if PROJECT_ID is set)"
