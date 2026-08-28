export const Status = { PENDING: "pending", RUNNING: "running", DONE: "done" };

export class Entity {
  constructor(id, name) {
    this.id = id;
    this.name = name;
    this.status = Status.PENDING;
    this.meta = new Map();
  }
  isDone() { return this.status === Status.DONE; }
  toJSON() { return { id: this.id, name: this.name, status: this.status }; }
}

export class Repository {
  constructor() { this.store = new Map(); }
  async find(id) { return this.store.get(id) ?? null; }
  async save(entity) { this.store.set(entity.id, entity); }
  findAll(ids) { return ids.map(id => this.store.get(id)).filter(Boolean); }
}

export class Service {
  constructor(repo) { this.repo = repo; this.cache = new Map(); }
  async execute(id) {
    if (this.cache.has(id)) return this.cache.get(id);
    const e = await this.repo.find(id);
    if (!e) throw new Error(`not found ${id}`);
    e.status = Status.RUNNING;
    await this.repo.save(e);
    this.cache.set(id, e);
    return e;
  }
  async *stream(ids) {
    for (const id of ids) {
      const e = await this.execute(id);
      yield e.name.toUpperCase();
    }
  }
}

export async function loadConfig(path) {
  const res = await fetch(path);
  return res.json();
}

export function validate(name) { return typeof name === "string" && name.length > 0; }

if (import.meta.main) {
  const svc = new Service(new Repository());
  console.log(await svc.execute("demo"));
}
