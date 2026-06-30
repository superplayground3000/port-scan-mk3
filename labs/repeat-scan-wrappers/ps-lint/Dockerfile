# Static-analysis image for the Windows deliverable. Since the .ps1 cannot execute
# against the Linux lab targets, this container runs PSScriptAnalyzer to prove the
# script parses and is clean — the containers-only way to validate Windows code.
FROM mcr.microsoft.com/powershell:7.4-alpine-3.17
RUN pwsh -NoProfile -Command "\
    Set-PSRepository -Name PSGallery -InstallationPolicy Trusted; \
    Install-Module -Name PSScriptAnalyzer -RequiredVersion 1.22.0 -Scope AllUsers -Force"
WORKDIR /lint
