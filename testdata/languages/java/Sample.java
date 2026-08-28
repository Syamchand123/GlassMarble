package com.example.sample;

import java.io.IOException;
import java.util.*;
import java.util.concurrent.CompletableFuture;
import java.util.stream.Collectors;

public class Sample {
    public enum Status { PENDING, RUNNING, DONE }

    public interface Repository<T> {
        Optional<T> findById(String id) throws IOException;
        void save(T entity);
        default List<T> findAll(Collection<String> ids) {
            return ids.stream().map(id -> {
                try { return findById(id).orElse(null); } catch (IOException e) { return null; }
            }).filter(Objects::nonNull).collect(Collectors.toList());
        }
    }

    public static class Entity {
        private final String id;
        private String name;
        private Status status = Status.PENDING;
        private Map<String, String> meta = new HashMap<>();

        public Entity(String id, String name) {
            this.id = Objects.requireNonNull(id);
            this.name = name;
        }
        public String getId() { return id; }
        public String getName() { return name; }
        public void setName(String name) { this.name = name; }
        public Status getStatus() { return status; }
        public void setStatus(Status s) { this.status = s; }
        @Override public String toString() { return "Entity{id=" + id + "}"; }
    }

    public static class Service {
        private final Repository<Entity> repo;
        private final List<Entity> cache = new ArrayList<>();

        public Service(Repository<Entity> repo) { this.repo = repo; }

        public CompletableFuture<Entity> execute(String id) {
            return CompletableFuture.supplyAsync(() -> {
                try {
                    Entity e = repo.findById(id).orElseThrow(() -> new NoSuchElementException(id));
                    e.setStatus(Status.RUNNING);
                    repo.save(e);
                    cache.add(e);
                    return e;
                } catch (IOException ex) {
                    throw new RuntimeException(ex);
                }
            });
        }

        public <T> List<T> map(List<Entity> in, java.util.function.Function<Entity, T> fn) {
            return in.stream().map(fn).collect(Collectors.toList());
        }
    }

    private final String name;
    public Sample(String name) { this.name = name; }
    public boolean run() throws IOException {
        if (name == null || name.isEmpty()) throw new IllegalArgumentException("name");
        return true;
    }
    public static void main(String[] args) throws Exception {
        Sample s = new Sample("demo");
        System.out.println(s.run());
    }
}
