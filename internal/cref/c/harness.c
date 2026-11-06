/*
 * harness.c - drives upstream TRE (built as C) for the differential tests of
 * the Go port. Protocol, one request per line on stdin, fields separated by
 * a single space, strings hex-encoded (a UTF-8 string in wide mode, bytes in
 * byte mode; "-" is the empty string):
 *
 *   <mode> <cflags> <eflags> <nmatch> <pattern> <subject>
 *
 * mode is 'w' (wide characters: tre_regwncomp/tre_regwnexec, offsets in
 * characters) or 'b' (bytes: tre_regncompb/tre_regnexecb). The reply is
 *   ERR <code>                    compilation failed
 *   NOMATCH
 *   OK <nsub> <so>,<eo> ...       one pair per requested submatch
 */
#include <locale.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

#include "tre/tre.h"

static int hexv(int c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return c - 'a' + 10;
  return c - 'A' + 10;
}

static unsigned char *unhex(const char *s, size_t *n) {
  size_t l = strlen(s), i;
  unsigned char *b = malloc(l / 2 + 1);
  if (l == 1 && s[0] == '-') { *n = 0; b[0] = 0; return b; }
  for (i = 0; i + 1 < l; i += 2) b[i / 2] = (unsigned char)(hexv(s[i]) * 16 + hexv(s[i + 1]));
  *n = l / 2;
  b[*n] = 0;
  return b;
}

/* Decodes UTF-8 (assumed valid) into wide characters. */
static wchar_t *widen(const unsigned char *b, size_t n, size_t *wn) {
  wchar_t *w = malloc((n + 1) * sizeof(wchar_t));
  size_t i = 0, k = 0;
  while (i < n) {
    unsigned c = b[i];
    wchar_t r;
    int extra;
    if (c < 0x80) { r = c; extra = 0; }
    else if (c < 0xe0) { r = c & 0x1f; extra = 1; }
    else if (c < 0xf0) { r = c & 0x0f; extra = 2; }
    else { r = c & 0x07; extra = 3; }
    i++;
    while (extra-- > 0 && i < n) r = (r << 6) | (b[i++] & 0x3f);
    w[k++] = r;
  }
  w[k] = 0;
  *wn = k;
  return w;
}

int main(void) {
  char *line = NULL;
  size_t cap = 0;
  if (!setlocale(LC_ALL, "en_US.UTF-8") && !setlocale(LC_ALL, "C.UTF-8")) {
    fprintf(stderr, "harness: no UTF-8 locale\n");
    return 2;
  }
  setvbuf(stdout, NULL, _IOLBF, 0);
  while (getline(&line, &cap, stdin) > 0) {
    char mode, pat[1 << 20], sub[1 << 20];
    int cflags, eflags, nmatch;
    regex_t re;
    regmatch_t pm[64];
    size_t pn, sn, wpn, wsn;
    unsigned char *pb, *sb;
    int rc, i;
    if (sscanf(line, "%c %d %d %d %1048575s %1048575s", &mode, &cflags, &eflags, &nmatch, pat, sub) != 6)
      continue;
    if (nmatch > 63) nmatch = 63;
    pb = unhex(pat, &pn);
    sb = unhex(sub, &sn);
    if (mode == 'w') {
      wchar_t *wp = widen(pb, pn, &wpn), *ws = widen(sb, sn, &wsn);
      rc = tre_regwncomp(&re, wp, wpn, cflags);
      if (rc == 0) {
        rc = tre_regwnexec(&re, ws, wsn, nmatch, pm, eflags);
      }
      free(wp); free(ws);
    } else {
      /* tre_regncompb copies exactly n bytes without a terminating NUL and
         the parser reads one past the end (heap over-read upstream): use the
         NUL-terminating tre_regcompb, the pattern has no NUL. */
      rc = tre_regcompb(&re, (const char *)pb, cflags);
      if (rc == 0) {
        rc = tre_regnexecb(&re, (const char *)sb, sn, nmatch, pm, eflags);
      }
    }
    if (rc != 0 && re.value == NULL) {
      printf("ERR %d\n", rc);
    } else if (rc != 0) {
      printf("NOMATCH\n");
      tre_regfree(&re);
    } else {
      printf("OK %d", (int)re.re_nsub);
      for (i = 0; i < nmatch; i++) printf(" %d,%d", (int)pm[i].rm_so, (int)pm[i].rm_eo);
      printf("\n");
      tre_regfree(&re);
    }
    free(pb); free(sb);
  }
  return 0;
}
