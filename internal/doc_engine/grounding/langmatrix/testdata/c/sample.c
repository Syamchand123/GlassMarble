#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>
/** Service settings loaded from the environment. */
struct config { char addr[64]; char port[16]; };
/** Load settings from the environment. */
struct config load_config(void) {
  struct config c;
  const char *a = getenv("SAMPLE_ADDR");
  const char *p = getenv("SAMPLE_PORT");
  snprintf(c.addr, sizeof c.addr, "%s", a ? a : "localhost");
  snprintf(c.port, sizeof c.port, "%s", p ? p : "8080");
  return c;
}
/** Format a greeting; returns -1 after logging on empty input. */
int format_greeting(const char *name, char *out, size_t n) {
  if (!name || !*name) { fprintf(stderr, "greet: empty name\n"); return -1; }
  return snprintf(out, n, "hello %s", name);
}
/** Greet one name on a background thread (call edge to format_greeting). */
void *greet_thread(void *arg) {
  char buf[128];
  format_greeting((const char *)arg, buf, sizeof buf);
  return NULL;
}
/** Greet a name via a joined pthread. */
void greet_background(const char *name) {
  pthread_t th;
  pthread_create(&th, NULL, greet_thread, (void *)name);
  pthread_join(th, NULL);
}
/** Test-looking function exercising format_greeting. */
void test_format_greeting(void) {
  char buf[128];
  int rc = format_greeting("ada", buf, sizeof buf);
  if (rc < 0 || strcmp(buf, "hello ada") != 0) { fprintf(stderr, "bad greet\n"); exit(1); }
}
