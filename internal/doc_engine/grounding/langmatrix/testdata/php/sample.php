<?php
declare(strict_types=1);

/** Service settings loaded from the environment. */
final class Config {
  public function __construct(
    public readonly string $addr = "localhost",
    public readonly string $port = "8080",
  ) {}
  /** Build settings from the environment. */
  public static function fromEnv(): self {
    $addr = getenv("SAMPLE_ADDR");
    return new self(is_string($addr) ? $addr : "localhost");
  }
}

/** Format a greeting; throws on empty input. */
function format_greeting(string $name): string {
  if ($name === "") { throw new InvalidArgumentException("greet: empty name"); }
  return "hello " . $name;
}

/** Greet one name; keeps a call edge from greet_all. */
function greet_one(string $name): string { return format_greeting($name); }

/** Greet a name inside a fiber (PHP 8.1+ concurrency). */
function greet_async(string $name): string {
  $fiber = new Fiber(function () use ($name) { return format_greeting($name); });
  return $fiber->start();
}

/** Test-looking function exercising format_greeting. */
function test_format_greeting(): void {
  assert(format_greeting("ada") === "hello ada");
}
