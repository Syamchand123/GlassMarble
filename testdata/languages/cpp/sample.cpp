#include <iostream>
#include <string>
#include <vector>
#include <memory>
#include <unordered_map>
#include <future>
#include <optional>

enum class Status { Pending, Running, Done };

template<typename T>
class Repository {
public:
    virtual ~Repository() = default;
    virtual std::optional<T> find(const std::string& id) = 0;
    virtual void save(const T& e) = 0;
};

struct Entity {
    std::string id;
    std::string name;
    Status status = Status::Pending;
    std::unordered_map<std::string,std::string> meta;
    explicit Entity(std::string i, std::string n): id(std::move(i)), name(std::move(n)) {}
};

class Service {
    std::shared_ptr<Repository<Entity>> repo;
    std::unordered_map<std::string, Entity> cache;
public:
    explicit Service(std::shared_ptr<Repository<Entity>> r): repo(std::move(r)) {}
    std::future<std::optional<Entity>> executeAsync(const std::string& id) {
        return std::async(std::launch::async, [this, id]() -> std::optional<Entity> {
            auto opt = repo->find(id);
            if (!opt) return std::nullopt;
            opt->status = Status::Running;
            repo->save(*opt);
            cache.emplace(id, *opt);
            return opt;
        });
    }
    template<typename U>
    std::vector<U> mapEntities(const std::vector<Entity>& in, std::function<U(const Entity&)> fn) {
        std::vector<U> out; out.reserve(in.size());
        for (auto& e: in) out.push_back(fn(e));
        return out;
    }
};

class Engine {
public:
    void start() { std::cout << "engine start\n"; }
    virtual void stop() {}
    virtual ~Engine() = default;
};

class Worker : public Engine {
    std::vector<std::future<void>> tasks;
public:
    void stop() override { std::cout << "worker stop\n"; }
    void submit(std::function<void()> fn) { tasks.push_back(std::async(fn)); }
};

int main() {
    auto svc = std::make_shared<Service>(nullptr);
    std::cout << "ok\n";
    return 0;
}
