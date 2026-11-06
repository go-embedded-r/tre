/*
 * recorder.c - wraps the regcomp/regexec entry points of upstream TRE so that
 * running TRE's own tests (tests/retest.c, narrow and wide) records every
 * call and its result. The Go tests replay the recording against the port.
 *
 * Line formats (code points as comma separated hex, "-" for none):
 *   C <w|n> <cflags> <pattern> <ret>
 *   E <w|n> <eflags> <nmatch> <subject> <ret> <so,eo>...
 * 'w' is a wide call, 'n' a narrow call.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

#include "tre/tre.h"

extern int real_regncomp(regex_t *, const char *, size_t, int);
extern int real_regcomp(regex_t *, const char *, int);
extern int real_regnexec(const regex_t *, const char *, size_t, size_t, regmatch_t[], int);
extern int real_regexec(const regex_t *, const char *, size_t, regmatch_t[], int);
extern int real_regwncomp(regex_t *, const wchar_t *, size_t, int);
extern int real_regwcomp(regex_t *, const wchar_t *, int);
extern int real_regwnexec(const regex_t *, const wchar_t *, size_t, size_t, regmatch_t[], int);
extern int real_regwexec(const regex_t *, const wchar_t *, size_t, regmatch_t[], int);

static FILE *out(void) {
  static FILE *f;
  if (!f) f = fopen(getenv("TRE_RECORD"), "a");
  return f;
}

static void put(FILE *f, const void *s, size_t n, int wide) {
  size_t i;
  if (n == 0) { fputs("-", f); return; }
  for (i = 0; i < n; i++) {
    if (i) fputc(',', f);
    fprintf(f, "%x", wide ? (unsigned)((const wchar_t *)s)[i] : (unsigned)((const unsigned char *)s)[i]);
  }
}

static void comp(int wide, const void *s, size_t n, int cflags, int ret) {
  FILE *f = out();
  fprintf(f, "C %c %d ", wide ? 'w' : 'n', cflags);
  put(f, s, n, wide);
  fprintf(f, " %d\n", ret);
}

static void exe(int wide, const void *s, size_t n, size_t nm, regmatch_t *pm, int eflags, int ret) {
  FILE *f = out();
  size_t i;
  fprintf(f, "E %c %d %d ", wide ? 'w' : 'n', eflags, (int)(pm ? nm : 0));
  put(f, s, n, wide);
  fprintf(f, " %d", ret);
  if (ret == 0 && pm)
    for (i = 0; i < nm; i++) fprintf(f, " %d,%d", (int)pm[i].rm_so, (int)pm[i].rm_eo);
  fputc('\n', f);
}

int tre_regncomp(regex_t *p, const char *s, size_t n, int c) {
  int r = real_regncomp(p, s, n, c); comp(0, s, n, c, r); return r;
}
int tre_regcomp(regex_t *p, const char *s, int c) {
  int r = real_regcomp(p, s, c); comp(0, s, s ? strlen(s) : 0, c, r); return r;
}
int tre_regwncomp(regex_t *p, const wchar_t *s, size_t n, int c) {
  int r = real_regwncomp(p, s, n, c); comp(1, s, n, c, r); return r;
}
int tre_regwcomp(regex_t *p, const wchar_t *s, int c) {
  int r = real_regwcomp(p, s, c); comp(1, s, s ? wcslen(s) : 0, c, r); return r;
}
int tre_regnexec(const regex_t *p, const char *s, size_t n, size_t nm, regmatch_t pm[], int e) {
  int r = real_regnexec(p, s, n, nm, pm, e); exe(0, s, s ? n : 0, nm, pm, e, r); return r;
}
int tre_regexec(const regex_t *p, const char *s, size_t nm, regmatch_t pm[], int e) {
  int r = real_regexec(p, s, nm, pm, e); exe(0, s, strlen(s), nm, pm, e, r); return r;
}
int tre_regwnexec(const regex_t *p, const wchar_t *s, size_t n, size_t nm, regmatch_t pm[], int e) {
  int r = real_regwnexec(p, s, n, nm, pm, e); exe(1, s, s ? n : 0, nm, pm, e, r); return r;
}
int tre_regwexec(const regex_t *p, const wchar_t *s, size_t nm, regmatch_t pm[], int e) {
  int r = real_regwexec(p, s, nm, pm, e); exe(1, s, wcslen(s), nm, pm, e, r); return r;
}
