/** Service settings loaded from the environment. */
interface Config { addr: string; port: number; }

/** Load settings from the environment. */
export function loadConfig(): Config {
  return { addr: process.env.SAMPLE_ADDR ?? "localhost", port: Number(process.env.SAMPLE_PORT ?? "8080") };
}

/** Format a greeting; throws on empty input. */
export function formatGreeting(name: string): string {
  if (!name) { throw new Error("greet: empty name"); }
  return `hello ${name}`;
}

/** Greet one name; keeps a call edge from greetAll. */
export async function greetOne(name: string): Promise<string> {
  return formatGreeting(name);
}

/** Greet every name concurrently. */
export async function greetAll(names: string[]): Promise<string[]> {
  return Promise.all(names.map((n) => greetOne(n)));
}

/** Test-looking function exercising formatGreeting. */
export function testFormatGreeting(): void {
  test("greets ada", () => assert(formatGreeting("ada")));
}
