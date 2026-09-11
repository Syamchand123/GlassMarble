import scala.concurrent.{Await, ExecutionContext, Future}
import scala.concurrent.duration.DurationInt
import scala.util.{Failure, Success, Try}

/** Service settings loaded from the environment. */
final case class Config(addr: String, port: String)

/** Load settings from the environment. */
def loadConfig(): Config = Config(
  addr = sys.env.getOrElse("SAMPLE_ADDR", "localhost"),
  port = sys.env.getOrElse("SAMPLE_PORT", "8080")
)

/** Format a greeting; throws on empty input. */
def formatGreeting(name: String): String = {
  require(name.nonEmpty, "greet: empty name")
  s"hello $name"
}

/** Greet one name; keeps a call edge from greetAll. */
def greetOne(name: String)(implicit ec: ExecutionContext): Future[String] =
  Future { formatGreeting(name) }

/** Greet every name concurrently. */
def greetAll(names: List[String])(implicit ec: ExecutionContext): List[String] = {
  Await.result(Future.traverse(names)(greetOne), 5.seconds)
}

/** Test-looking function exercising formatGreeting. */
def testFormatGreeting()(implicit ec: ExecutionContext): Unit = {
  assert(formatGreeting("ada") == "hello ada")
}
