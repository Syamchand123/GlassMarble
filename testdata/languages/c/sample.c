#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

#define MAX_NAME 64
#define CACHE_SIZE 256

typedef enum { STATUS_PENDING, STATUS_RUNNING, STATUS_DONE } Status;

typedef struct {
    char id[32];
    char name[MAX_NAME];
    Status status;
    char meta[128];
} Entity;

typedef struct Node {
    Entity data;
    struct Node* next;
} Node;

typedef struct {
    Node* head;
    pthread_mutex_t mu;
    int size;
} Cache;

Cache* cache_create(void) {
    Cache* c = calloc(1, sizeof(Cache));
    pthread_mutex_init(&c->mu, NULL);
    return c;
}

void cache_put(Cache* c, Entity e) {
    pthread_mutex_lock(&c->mu);
    Node* n = malloc(sizeof(Node));
    n->data = e;
    n->next = c->head;
    c->head = n;
    c->size++;
    pthread_mutex_unlock(&c->mu);
}

Entity* cache_get(Cache* c, const char* id) {
    pthread_mutex_lock(&c->mu);
    for (Node* cur = c->head; cur; cur = cur->next) {
        if (strcmp(cur->data.id, id) == 0) {
            pthread_mutex_unlock(&c->mu);
            return &cur->data;
        }
    }
    pthread_mutex_unlock(&c->mu);
    return NULL;
}

int process_entity(Entity* e) {
    if (!e) return -1;
    e->status = STATUS_RUNNING;
    snprintf(e->meta, sizeof(e->meta), "processed:%s", e->name);
    return 0;
}

int main(void) {
    Cache* c = cache_create();
    Entity e = {.status = STATUS_PENDING};
    strncpy(e.id, "123", sizeof(e.id));
    strncpy(e.name, "demo", sizeof(e.name));
    cache_put(c, e);
    Entity* got = cache_get(c, "123");
    if (got) process_entity(got);
    printf("size=%d\n", c->size);
    return 0;
}
