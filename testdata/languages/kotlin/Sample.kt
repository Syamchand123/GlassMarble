package sample

import kotlinx.coroutines.*

enum class Status { PENDING, RUNNING, DONE }

data class Entity(val id: String, var name: String, var status: Status = Status.PENDING, val meta: MutableMap<String,String> = mutableMapOf())

interface Repository<T> {
    suspend fun find(id: String): T?
    suspend fun save(entity: T)
    suspend fun findAll(ids: Collection<String>): List<T> = ids.mapNotNull { find(it) }
}

class Service(private val repo: Repository<Entity>) {
    private val cache = mutableMapOf<String, Entity>()
    suspend fun execute(id: String): Entity {
        cache[id]?.let { return it }
        val e = repo.find(id) ?: throw NoSuchElementException(id)
        e.status = Status.RUNNING
        repo.save(e)
        cache[id] = e
        return e
    }
    fun <U> map(entities: List<Entity>, fn: (Entity) -> U): List<U> = entities.map(fn)
}

data class User(val name: String, val age: Int)
class UserService {
    fun getUser(): User = User("Alice", 30)
    suspend fun fetchAsync(id: String): Entity = withContext(Dispatchers.IO) { Entity(id, "async") }
}

sealed class Result<out T> {
    data class Ok<T>(val value: T) : Result<T>()
    data class Err(val msg: String) : Result<Nothing>()
}

fun validate(name: String): Boolean = name.isNotBlank()
