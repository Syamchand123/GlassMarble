//! Grounding fixture for the depth matrix.
use std::{collections::HashMap, env, sync::{Arc, Mutex}, thread};

/// Service settings loaded from the environment.
pub struct Config { pub addr: String, pub port: String }

/// Load settings from the environment.
pub fn load_config() -> Config {
    Config {
        addr: env::var("SAMPLE_ADDR").unwrap_or_else(|_| "localhost".into()),
        port: env::var("SAMPLE_PORT").unwrap_or_else(|_| "8080".into()),
    }
}

/// Format a greeting; errors on empty input.
pub fn format_greeting(name: &str) -> Result<String, String> {
    if name.is_empty() { return Err("greet: empty name".into()); }
    Ok(format!("hello {name}"))
}

/// Greet one name; keeps a call edge from greet_all.
pub fn greet_one(name: &str) -> Result<String, String> { format_greeting(name) }

/// Greet every name on scoped background threads.
pub fn greet_all(names: Vec<String>) -> Vec<String> {
    let out = Arc::new(Mutex::new(HashMap::new()));
    thread::scope(|s| {
        for n in &names {
            let out = Arc::clone(&out);
            s.spawn(move || { out.lock().unwrap().insert(n.clone(), greet_one(n)); });
        }
    });
    names.iter().map(|n| out.lock().unwrap().remove(n).unwrap().unwrap_or_default()).collect()
}

#[test]
fn test_format_greeting() { assert_eq!(format_greeting("ada").unwrap(), "hello ada"); }
