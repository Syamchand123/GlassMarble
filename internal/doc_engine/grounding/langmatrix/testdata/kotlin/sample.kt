import java.util.concurrent.Executors

/** Service settings loaded from the environment. */
data class Config(val addr: String, val port: String)

/** Load settings from the environment. */
fun loadConfig(): Config = Config(
  addr = System.getenv("SAMPLE_ADDR") ?: "localhost",
  port = System.getenv("SAMPLE_PORT") ?: "8080",
)

/** Format a greeting; throws on empty input. */
fun formatGreeting(name: String): String {
  require(name.isNotEmpty()) { "greet: empty name" }
  return "hello $name"
}

/** Greet one name; keeps a call edge from greetAll. */
fun greetOne(name: String): String = formatGreeting(name)

/** Greet every name on a background thread pool. */
fun greetAll(names: List<String>): List<String> {
  val pool = Executors.newFixedThreadPool(4)
  try {
    return pool.invokeAll(names.map { n -> java.util.concurrent.Callable { greetOne(n) } }).map { it.get() }
  } finally { pool.shutdown() }
}

/** Test-looking function exercising formatGreeting. */
@Test fun testFormatGreeting() { assert(formatGreeting("ada") == "hello ada") }
