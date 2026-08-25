data class User(val name: String, val age: Int)

class UserService {
    fun getUser(): User = User("Alice", 30)
}
