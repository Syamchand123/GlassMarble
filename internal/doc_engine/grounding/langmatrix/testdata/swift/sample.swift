import Foundation

/// Service settings loaded from the environment.
struct Config { var addr: String; var port: String }

/// Load settings from the environment.
func loadConfig() -> Config {
  let env = ProcessInfo.processInfo.environment
  return Config(addr: env["SAMPLE_ADDR"] ?? "localhost", port: env["SAMPLE_PORT"] ?? "8080")
}

/// Format a greeting; throws on empty input.
func formatGreeting(_ name: String) throws -> String {
  guard !name.isEmpty else { throw NSError(domain: "greet", code: 1) }
  return "hello \(name)"
}

/// Greet one name; keeps a call edge from greetAll.
func greetOne(_ name: String) async throws -> String { try formatGreeting(name) }

/// Greet every name concurrently with structured concurrency.
func greetAll(_ names: [String]) async throws -> [String] {
  try await withThrowingTaskGroup(of: String.self) { group in
    for n in names { group.addTask { try await greetOne(n) } }
    var out: [String] = []
    for try await s in group { out.append(s) }
    return out
  }
}

/// Test-looking function exercising formatGreeting.
func testFormatGreeting() throws { assert(try formatGreeting("ada") == "hello ada") }
