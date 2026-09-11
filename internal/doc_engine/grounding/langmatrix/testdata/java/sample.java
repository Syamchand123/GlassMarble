import java.util.*;
import java.util.concurrent.*;

/** Service settings loaded from the environment. */
class Config {
  final String addr = System.getenv("SAMPLE_ADDR");
  final String port = System.getProperty("sample.port", "8080");
}

/** Greeter service with documented members. */
public class Greeter {
  /** Format a greeting; throws on empty input. */
  public static String greet(String name) {
    if (name == null || name.isEmpty()) {
      throw new IllegalArgumentException("greet: empty name");
    }
    return "hello " + name;
  }
  /** Greet one name; keeps a call edge from greetAll. */
  public static String greetOne(String name) { return greet(name); }
  /** Greet every name on a thread pool. */
  public static List<String> greetAll(List<String> names) throws Exception {
    ExecutorService pool = Executors.newFixedThreadPool(4);
    try {
      List<Future<String>> fs = new ArrayList<>();
      for (String n : names) { fs.add(pool.submit(() -> greet(n))); }
      List<String> out = new ArrayList<>();
      for (Future<String> f : fs) { out.add(f.get()); }
      return out;
    } finally { pool.shutdown(); }
  }
  /** Test-looking method exercising greet. */
  @Test public static void testGreet() { assert greet("ada").equals("hello ada"); }
}
