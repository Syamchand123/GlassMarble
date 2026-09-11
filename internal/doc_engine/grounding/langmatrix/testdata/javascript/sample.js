/** Service settings loaded from the environment. */
class Config {
  constructor() {
    this.addr = process.env.SAMPLE_ADDR || "localhost";
    this.port = process.env.SAMPLE_PORT || "8080";
  }
}

/** Format a greeting for name; throws on empty input. */
function formatGreeting(name) {
  if (!name) {
    throw new Error("greet: empty name");
  }
  return "hello " + name;
}

/** Greet one name; keeps a call edge from greetAll. */
async function greetOne(name) {
  return formatGreeting(name);
}

/** Greet every name concurrently. */
async function greetAll(names) {
  return Promise.all(names.map((n) => greetOne(n)));
}

/** Test-looking function exercising formatGreeting. */
function testFormatGreeting() {
  test("greets ada", () => {
    assert(formatGreeting("ada") === "hello ada");
  });
}
