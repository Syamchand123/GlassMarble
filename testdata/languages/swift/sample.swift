protocol Printable {
    func printDetails()
}

class Document: Printable {
    var title: String = ""
    func printDetails() {}
}
