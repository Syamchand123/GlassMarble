using System;
using System.Linq;
using System.Threading.Tasks;

/** Service settings loaded from the environment. */
public sealed class Config {
  public string Addr { get; init; } = Environment.GetEnvironmentVariable("SAMPLE_ADDR") ?? "localhost";
  public string Port { get; init; } = Environment.GetEnvironmentVariable("SAMPLE_PORT") ?? "8080";
}

/** Greeting helpers with documented members. */
public static class Greeter {
  /** Format a greeting; throws on empty input. */
  public static string FormatGreeting(string name) {
    if (string.IsNullOrEmpty(name)) { throw new ArgumentException("greet: empty name"); }
    return "hello " + name;
  }
  /** Greet one name; keeps a call edge from GreetAll. */
  public static string GreetOne(string name) => FormatGreeting(name);
  /** Greet every name concurrently. */
  public static async Task<string[]> GreetAll(string[] names) {
    return await Task.WhenAll(names.Select(async n => GreetOne(n)));
  }
  /** Test-looking method exercising FormatGreeting. */
  [Test] public static void TestFormatGreeting() {
    if (FormatGreeting("ada") != "hello ada") { throw new Exception("bad greet"); }
  }
}
