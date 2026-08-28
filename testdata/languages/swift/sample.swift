import Foundation

enum Status: String, Codable { case pending, running, done }

protocol Repository {
    associatedtype T
    func find(id: String) async throws -> T?
    func save(_ entity: T) async throws
}

struct Entity: Codable, Identifiable {
    let id: String
    var name: String
    var status: Status = .pending
    var meta: [String:String] = [:]
}

actor Cache {
    private var store: [String: Entity] = [:]
    func get(_ id: String) -> Entity? { store[id] }
    func set(_ e: Entity) { store[e.id] = e }
}

class Service {
    let cache = Cache()
    func execute(id: String) async throws -> Entity {
        if let cached = await cache.get(id) { return cached }
        var e = Entity(id: id, name: "demo")
        e.status = .running
        await cache.set(e)
        return e
    }
    func validate(name: String) -> Bool { !name.isEmpty }
}

protocol Printable {
    func printDetails()
}

class Document: Printable {
    var title: String = ""
    var body: String = ""
    func printDetails() { print("\(title): \(body)") }
    func save(to path: String) throws {
        let data = try JSONEncoder().encode(Entity(id: "1", name: title))
        try data.write(to: URL(fileURLWithPath: path))
    }
}

enum AppError: Error { case notFound, invalidInput }
