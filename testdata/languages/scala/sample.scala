import scala.concurrent.{Future, ExecutionContext}
import scala.util.{Try, Success, Failure}

sealed trait Status
case object Pending extends Status
case object Running extends Status
case object Done extends Status

case class Entity(id: String, name: String, status: Status = Pending, meta: Map[String,String] = Map.empty)
case class Message(id: Long, text: String)

trait Repository[T] {
  def find(id: String): Option[T]
  def save(e: T): Try[Unit]
  def findAll(ids: Seq[String]): Seq[T] = ids.flatMap(find)
}

class Service(repo: Repository[Entity])(implicit ec: ExecutionContext) {
  private val cache = scala.collection.mutable.Map[String, Entity]()
  def execute(id: String): Future[Entity] = Future {
    cache.get(id) match {
      case Some(e) => e
      case None =>
        val e = repo.find(id).getOrElse(throw new NoSuchElementException(id))
        val updated = e.copy(status = Running)
        repo.save(updated).get
        cache.put(id, updated)
        updated
    }
  }
  def mapEntities[U](entities: Seq[Entity])(fn: Entity => U): Seq[U] = entities.map(fn)
}

class MessageHandler {
  def handle(msg: Message): Unit = println(s"handle ${msg.text}")
  def handleAsync(msg: Message)(implicit ec: ExecutionContext): Future[Unit] = Future { handle(msg) }
}

object App extends App {
  val svc = new Service(new Repository[Entity] {
    def find(id: String) = Some(Entity(id, "demo"))
    def save(e: Entity) = Success(())
  })
  println("ready")
}
