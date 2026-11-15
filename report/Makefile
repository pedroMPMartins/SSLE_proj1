# Makefile for compiling the SSLE Project 1 LaTeX Report

MAIN = main
TEX_FILES = $(wildcard sections/*.tex) $(MAIN).tex references.bib

all: $(MAIN).pdf

$(MAIN).pdf: $(TEX_FILES)
	pdflatex -interaction=nonstopmode -file-line-error $(MAIN).tex
	bibtex $(MAIN)
	pdflatex -interaction=nonstopmode -file-line-error $(MAIN).tex
	pdflatex -interaction=nonstopmode -file-line-error $(MAIN).tex

clean:
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

distclean: clean
	rm -f $(MAIN).pdf

.PHONY: all clean distclean
