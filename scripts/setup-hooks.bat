@echo off
setlocal enabledelayedexpansion

for /f "delims=" %%i in ('git rev-parse --show-toplevel') do set "REPO_ROOT=%%i"
cd /d "%REPO_ROOT%"

git config core.hooksPath .githooks
echo git hooks path set to .githooks
