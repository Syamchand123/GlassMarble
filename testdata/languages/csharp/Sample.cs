using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using System.Text.Json;

namespace SampleNamespace
{
    public enum Status { Pending, Running, Done }

    public interface IRepository<T> where T : class
    {
        Task<T?> FindAsync(string id);
        Task SaveAsync(T entity);
    }

    public record Entity(string Id, string Name)
    {
        public Status Status { get; set; } = Status.Pending;
        public Dictionary<string,string> Meta { get; init; } = new();
    }

    public class Service
    {
        private readonly IRepository<Entity> _repo;
        private readonly Dictionary<string, Entity> _cache = new();

        public Service(IRepository<Entity> repo) => _repo = repo;

        public async Task<Entity> ExecuteAsync(string id)
        {
            if (_cache.TryGetValue(id, out var cached)) return cached;
            var e = await _repo.FindAsync(id) ?? throw new KeyNotFoundException(id);
            e.Status = Status.Running;
            await _repo.SaveAsync(e);
            _cache[id] = e;
            return e;
        }

        public string ToJson(Entity e) => JsonSerializer.Serialize(e);
    }

    public class SampleClass
    {
        public string Name { get; set; } = "";
        public void Execute() => Console.WriteLine($"Hello {Name}");
        public static async Task Main() {
            var svc = new Service(null!);
            Console.WriteLine("ready");
        }
    }
}
