#include <cstdlib>
#include <iostream>
#include <string>
#include <thread>
#include <vector>
/** Service settings loaded from the environment. */
struct Config { std::string addr = "localhost"; std::string port = "8080"; };

/** Load settings from the environment. */
Config load_config() {
  Config c;
  if (const char *a = std::getenv("SAMPLE_ADDR")) { c.addr = a; }
  if (const char *p = std::getenv("SAMPLE_PORT")) { c.port = p; }
  return c;
}

/** Format a greeting; throws on empty input. */
std::string format_greeting(const std::string &name) {
  if (name.empty()) { throw std::runtime_error("greet: empty name"); }
  return "hello " + name;
}

/** Greet one name; keeps a call edge from greet_all. */
std::string greet_one(const std::string &name) { return format_greeting(name); }

/** Greet every name on background threads. */
std::vector<std::string> greet_all(const std::vector<std::string> &names) {
  std::vector<std::string> out(names.size());
  std::vector<std::thread> workers;
  for (size_t i = 0; i < names.size(); ++i) {
    workers.emplace_back([&, i] { out[i] = greet_one(names[i]); });
  }
  for (auto &t : workers) { t.join(); }
  return out;
}

/** Test-looking function exercising format_greeting. */
void test_format_greeting() {
  if (format_greeting("ada") != "hello ada") { std::cerr << "bad greet\n"; std::abort(); }
}
