use kitup::{
    install_bundled_skill, plan_bundled_skill, AgentSelector, BaseOptions, InstallOptions, Scope,
};
use serde_json::Value;
use std::env;
use std::error::Error;
use std::fs;
use std::path::PathBuf;

fn main() -> Result<(), Box<dyn Error>> {
    let repo = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../..")
        .canonicalize()?;
    let sandbox = mktemp_dir("kitup-example-rust")?;
    let home = sandbox.join("home");
    let cwd = sandbox.join("workspace");

    fs::create_dir_all(&home)?;
    fs::create_dir_all(&cwd)?;
    println!("sandbox: {}", sandbox.display());

    let options = InstallOptions {
        base: BaseOptions {
            home: Some(home),
            cwd: Some(cwd),
            hosts_file: None,
        },
        app_id: "kitup-example-rust".to_string(),
        skill_dir: repo.join("skills/kitup"),
        scope: Scope::User,
        agents: AgentSelector::Explicit(vec!["codex".to_string()]),
    };

    let plan = plan_bundled_skill(&options)?;
    print_json("plan", &plan);
    expect_report(&plan, "installed", "plan did not find one install target");

    let install = install_bundled_skill(&options)?;
    print_json("install", &install);
    expect_report(&install, "installed", "install did not write one target");
    assert!(sandbox
        .join("home/.agents/skills/kitup/.kitup.json")
        .exists());

    let again = install_bundled_skill(&options)?;
    print_json("install again", &again);
    expect_report(
        &again,
        "skipped",
        "second install did not skip unchanged target",
    );
    assert_eq!(again["skipped"][0]["reason"], "unchanged");

    Ok(())
}

fn print_json(label: &str, value: &Value) {
    println!("{label}");
    println!("{}", serde_json::to_string_pretty(value).unwrap());
}

fn expect_report(value: &Value, key: &str, message: &str) {
    assert_eq!(value[key].as_array().unwrap().len(), 1, "{message}");
    assert_eq!(value["errors"].as_array().unwrap().len(), 0, "{message}");
}

fn mktemp_dir(prefix: &str) -> Result<PathBuf, Box<dyn Error>> {
    for attempt in 0..100 {
        let path = env::temp_dir().join(format!("{prefix}-{}-{attempt}", std::process::id()));
        match fs::create_dir(&path) {
            Ok(()) => return Ok(path),
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {}
            Err(error) => return Err(Box::new(error)),
        }
    }
    Err("failed to create temp dir".into())
}
