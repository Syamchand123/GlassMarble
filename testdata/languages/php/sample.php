<?php
declare(strict_types=1);

namespace App;

enum Status: string { case Pending = 'pending'; case Running = 'running'; case Done = 'done'; }

interface Repository {
    public function find(string $id): ?Entity;
    public function save(Entity $e): void;
}

final class Entity {
    public function __construct(
        public readonly string $id,
        public string $name,
        public Status $status = Status::Pending,
        public array $meta = []
    ) {}
    public function isDone(): bool { return $this->status === Status::Done; }
    public function toArray(): array { return ['id'=>$this->id,'name'=>$this->name,'status'=>$this->status->value]; }
}

final class Service {
    /** @var array<string,Entity> */
    private array $cache = [];
    public function __construct(private Repository $repo) {}
    public function execute(string $id): Entity {
        if (isset($this->cache[$id])) return $this->cache[$id];
        $e = $this->repo->find($id);
        if ($e === null) throw new \RuntimeException("not found $id");
        $e->status = Status::Running;
        $this->repo->save($e);
        $this->cache[$id] = $e;
        return $e;
    }
    public function validate(string $name): bool { return $name !== ''; }
}

final class Controller {
    public function __construct(private Service $svc) {}
    public function index(string $id): string {
        $e = $this->svc->execute($id);
        return json_encode($e->toArray());
    }
}
