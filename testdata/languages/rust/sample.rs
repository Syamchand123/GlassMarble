use std::collections::HashMap;
use std::sync::{Arc, Mutex};

#[derive(Debug, Clone, PartialEq)]
pub enum Status { Pending, Running, Done }

#[derive(Debug, Clone)]
pub struct Entity {
    pub id: String,
    pub name: String,
    pub status: Status,
    pub meta: HashMap<String, String>,
}

impl Entity {
    pub fn new(id: impl Into<String>, name: impl Into<String>) -> Self {
        Self { id: id.into(), name: name.into(), status: Status::Pending, meta: HashMap::new() }
    }
    pub fn is_done(&self) -> bool { self.status == Status::Done }
}

pub trait Repository: Send + Sync {
    fn find(&self, id: &str) -> Option<Entity>;
    fn save(&self, e: Entity) -> Result<(), String>;
}

pub struct Service {
    repo: Arc<dyn Repository>,
    cache: Mutex<HashMap<String, Entity>>,
}

impl Service {
    pub fn new(repo: Arc<dyn Repository>) -> Self {
        Self { repo, cache: Mutex::new(HashMap::new()) }
    }
    pub fn execute(&self, id: &str) -> Result<Entity, String> {
        if let Some(cached) = self.cache.lock().unwrap().get(id).cloned() {
            return Ok(cached);
        }
        let mut e = self.repo.find(id).ok_or_else(|| format!("not found {id}"))?;
        e.status = Status::Running;
        self.repo.save(e.clone())?;
        self.cache.lock().unwrap().insert(id.to_string(), e.clone());
        Ok(e)
    }
}

pub struct Worker {
    pub id: u64,
}

impl Worker {
    pub fn new(id: u64) -> Self { Worker { id } }
    pub fn run(&self) -> Result<(), String> { Ok(()) }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test] fn test_entity() { let e = Entity::new("1", "demo"); assert!(!e.is_done()); }
}
