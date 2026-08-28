require 'json'

module Sample
  class Status
    PENDING = :pending
    RUNNING = :running
    DONE = :done
  end

  class Entity
    attr_accessor :id, :name, :status, :meta
    def initialize(id, name)
      @id = id
      @name = name
      @status = Status::PENDING
      @meta = {}
    end
    def done?; @status == Status::DONE; end
    def to_h; { id: @id, name: @name, status: @status }; end
  end

  module Repository
    def find(id); raise NotImplementedError; end
    def save(entity); raise NotImplementedError; end
  end

  class Service
    include Repository
    def initialize(repo)
      @repo = repo
      @cache = {}
    end
    def execute(id)
      return @cache[id] if @cache.key?(id)
      e = @repo.find(id)
      raise "not found #{id}" unless e
      e.status = Status::RUNNING
      @repo.save(e)
      @cache[id] = e
      e
    end
    def map(entities, &block)
      entities.map(&block)
    end
  end

  class Account
    attr_accessor :balance
    def initialize(balance)
      @balance = balance
    end
    def deposit(amount)
      @balance += amount
      self
    end
    def withdraw(amount)
      raise 'insufficient' if amount > @balance
      @balance -= amount
    end
  end
end
