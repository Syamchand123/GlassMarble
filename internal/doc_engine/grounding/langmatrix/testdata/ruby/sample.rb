# Grounding fixture for the depth matrix.
require "net/http"

# Service settings loaded from the environment.
class Config
  attr_reader :addr, :port
  def initialize
    @addr = ENV.fetch("SAMPLE_ADDR", "localhost")
    @port = ENV.fetch("SAMPLE_PORT", "8080")
  end
end

# Format a greeting; raises on empty input.
def format_greeting(name)
  raise ArgumentError, "greet: empty name" if name.nil? || name.empty?
  "hello #{name}"
end

# Greet one name; keeps a call edge from greet_all.
def greet_one(name)
  format_greeting(name)
end

# Greet every name on background threads.
def greet_all(names)
  names.map { |n| Thread.new { greet_one(n) }.value }
end

# Test-looking function exercising format_greeting.
def test_format_greeting
  raise "bad greet" unless format_greeting("ada") == "hello ada"
end
