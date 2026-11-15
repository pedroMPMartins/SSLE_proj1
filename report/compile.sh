#!/usr/bin/env bash
# ==============================================================================
# Compile SSLE Project 1 LaTeX Report & Clean Build Artifacts
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GREEN='\033[0;32m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

cleanup() {
    echo -e "${CYAN}${BOLD}[CLEANUP] Purging all auxiliary LaTeX build artifacts...${NC}"
    find . -type f \( \
        -name "*.aux" -o \
        -name "*.bbl" -o \
        -name "*.blg" -o \
        -name "*.log" -o \
        -name "*.out" -o \
        -name "*.toc" -o \
        -name "*.fls" -o \
        -name "*.fdb_latexmk" -o \
        -name "*.synctex*" -o \
        -name "*.nav" -o \
        -name "*.snm" -o \
        -name "*.vrb" -o \
        -name "*.lof" -o \
        -name "*.lot" -o \
        -name "*.bcf" -o \
        -name "*.run.xml" -o \
        -name "*.idx" -o \
        -name "*.ilg" -o \
        -name "*.ind" -o \
        -name "*.dvi" \
    \) -delete
}

trap 'echo -e "\n${RED}${BOLD}[ERROR] Compilation failed. See terminal output above for details.${NC}"' ERR

echo -e "${BLUE}${BOLD}======================================================================${NC}"
echo -e "${BLUE}${BOLD}           Compiling SSLE Project 1 Report (LaTeX)                    ${NC}"
echo -e "${BLUE}${BOLD}======================================================================${NC}"

# Clean any existing stale artifacts before starting
cleanup

echo -e "\n${BLUE}${BOLD}[1/4] Running initial pdflatex pass...${NC}"
pdflatex -interaction=nonstopmode -file-line-error main.tex

echo -e "\n${BLUE}${BOLD}[2/4] Running bibtex for references...${NC}"
bibtex main

echo -e "\n${BLUE}${BOLD}[3/4] Resolving cross-references & TOC (pass 2 & 3)...${NC}"
pdflatex -interaction=nonstopmode -file-line-error main.tex
pdflatex -interaction=nonstopmode -file-line-error main.tex

echo -e "\n${BLUE}${BOLD}[4/4] Cleaning build artifacts...${NC}"
cleanup

echo -e "\n${GREEN}${BOLD}======================================================================${NC}"
echo -e "${GREEN}${BOLD}[SUCCESS] Compilation complete! 'main.pdf' is ready.${NC}"
echo -e "${GREEN}${BOLD}======================================================================${NC}"
ls -lh main.pdf
if command -v pdfinfo &>/dev/null; then
    echo -e "${CYAN}Pages:${NC} $(pdfinfo main.pdf | grep "Pages:" | awk '{print $2}')"
fi
