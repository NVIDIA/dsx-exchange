#!/bin/sh
set +e  # Continue on errors
export LANG=en_US.UTF-8
export TERM=xterm-256color

# Install required packages and tools first (before using bash features)
apk add --no-cache bash make git curl >/dev/null 2>&1
go install github.com/go-delve/delve/cmd/dlv@latest >/dev/null 2>&1
go install github.com/air-verse/air@latest >/dev/null 2>&1

COLOR_BLUE="\033[0;94m"
COLOR_GREEN="\033[0;92m"
COLOR_RESET="\033[0m"

# Print useful output for user
printf "${COLOR_GREEN}"
cat << 'EOF'
 _   _ __      __ _____  _____  _____
| \ | |\ \    / /|_   _||  __ \|_   _|   /\
|  \| | \ \  / /   | |  | |  | | | |    /  \
| . ` |  \ \/ /    | |  | |  | | | |   / /\ \
| |\  |   \  /    _| |_ | |__| |_| |_ / ____ \
|_| \_|    \/    |_____||_____/|_____/_/    \_\

EOF
printf "${COLOR_RESET}
Welcome to your development container!

This is how you can work with it:
- Files will be synchronized between your local machine and this container
- Ports forwarded: 5005 (debugger), 8000 (API), 9090 (metrics) → localhost
- Service accessible via ingress: http://auth-callout.127-0-0-1.nip.io:8080

Development commands available:
${COLOR_GREEN}→${COLOR_RESET} Run '${COLOR_GREEN}make dev${COLOR_RESET}' to start with hot reloading (recommended)
${COLOR_GREEN}→${COLOR_RESET} Run '${COLOR_GREEN}make dev-debug${COLOR_RESET}' to start with debugger (continues immediately)
${COLOR_GREEN}→${COLOR_RESET} Run '${COLOR_GREEN}make dev-debug-suspend${COLOR_RESET}' to start with debugger (waits for connection)
${COLOR_GREEN}→${COLOR_RESET} Run '${COLOR_GREEN}make run${COLOR_RESET}' to run normally without hot reloading
${COLOR_GREEN}→${COLOR_RESET} Run '${COLOR_GREEN}make test${COLOR_RESET}' to run tests
${COLOR_GREEN}→${COLOR_RESET} Run '${COLOR_GREEN}make lint${COLOR_RESET}' to run code analysis

Commands that get proxied to your host machine:
${COLOR_GREEN}→${COLOR_RESET} '${COLOR_GREEN}devspace${COLOR_RESET}', '${COLOR_GREEN}kubectl${COLOR_RESET}', '${COLOR_GREEN}k${COLOR_RESET}', '${COLOR_GREEN}helm${COLOR_RESET}', '${COLOR_GREEN}git${COLOR_RESET}' (executed on host when run from container)
"

# Set terminal prompt
export PS1="\[${COLOR_BLUE}\]devspace\[${COLOR_RESET}\] ./\W \[${COLOR_BLUE}\]\\$\[${COLOR_RESET}\] "
if [ -z "$BASH" ]; then export PS1="$ "; fi

# Include project's bin/ folder in PATH
export PATH="./bin:$PATH"

bash --norc
