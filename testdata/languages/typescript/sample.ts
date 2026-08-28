export enum Status { Pending = "pending", Running = "running", Done = "done" }

export interface Repository<T extends { id: string }> {
  find(id: string): Promise<T | null>;
  save(entity: T): Promise<void>;
  findAll(ids: string[]): Promise<T[]>;
}

export type Entity = {
  id: string;
  name: string;
  status: Status;
  meta: Record<string, string>;
};

export class Service implements Repository<Entity> {
  private store = new Map<string, Entity>();
  private cache = new Map<string, Entity>();

  async find(id: string): Promise<Entity | null> {
    return this.store.get(id) ?? null;
  }
  async save(e: Entity): Promise<void> { this.store.set(e.id, e); }

  async findAll(ids: string[]): Promise<Entity[]> {
    return (await Promise.all(ids.map(id => this.find(id)))).filter((e): e is Entity => e !== null);
  }

  async execute(id: string): Promise<Entity> {
    if (this.cache.has(id)) return this.cache.get(id)!;
    const e = await this.find(id);
    if (!e) throw new Error(`not found ${id}`);
    e.status = Status.Running;
    await this.save(e);
    this.cache.set(id, e);
    return e;
  }

  async *stream(ids: string[]): AsyncGenerator<string> {
    for (const id of ids) {
      const e = await this.execute(id);
      yield e.name.toUpperCase();
    }
  }
}

export function validate(name: unknown): name is string {
  return typeof name === "string" && name.length > 0;
}

export type Result<T> = { ok: true; value: T } | { ok: false; error: string };

export async function withRetry<T>(fn: () => Promise<T>, retries = 3): Promise<Result<T>> {
  for (let i = 0; i < retries; i++) {
    try { return { ok: true, value: await fn() }; } catch (e) { if (i === retries - 1) return { ok: false, error: String(e) }; }
  }
  return { ok: false, error: "unreachable" };
}
