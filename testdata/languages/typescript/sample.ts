interface Config {
    title: string;
}

class Manager implements Config {
    title: string;
    constructor(title: string) {
        this.title = title;
    }
}
