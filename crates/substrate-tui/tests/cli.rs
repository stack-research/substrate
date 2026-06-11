use assert_cmd::Command;
use predicates::prelude::*;
use tempfile::TempDir;

fn substrate(space: &TempDir) -> Command {
    let mut cmd = Command::cargo_bin("substrate").unwrap();
    // isolate machine-level config (~/.substrate) — init writes a registry
    cmd.env("SUBSTRATE_HOME", space.path().join(".home"))
        .arg("--space")
        .arg(space.path());
    cmd
}

fn set_up_lab(space: &TempDir) {
    substrate(space).arg("init").assert().success();
    for (name, kind) in [
        ("user-name", "human"),
        ("pat", "human"),
        ("claude-a", "agent"),
    ] {
        substrate(space)
            .args(["add", name, "--kind", kind])
            .assert()
            .success();
    }
    substrate(space)
        .args([
            "new",
            "lab",
            "--topic",
            "t",
            "--moderator",
            "user-name",
            "--turns",
            "claude-a,pat",
        ])
        .assert()
        .success();
}

#[test]
fn happy_path_round() {
    let space = TempDir::new().unwrap();
    set_up_lab(&space);

    substrate(&space)
        .args([
            "write",
            "lab",
            "--as",
            "user-name",
            "-m",
            "opening instructions",
        ])
        .assert()
        .success()
        .stdout(predicates::str::contains("next: claude-a"));
    substrate(&space)
        .args(["write", "lab", "--as", "claude-a", "-m", "hello room"])
        .assert()
        .success()
        .stdout(predicates::str::contains("next: pat"));
    substrate(&space)
        .args(["write", "lab", "--as", "pat", "-m", "pass"])
        .assert()
        .success()
        .stdout(predicates::str::contains("(no-op)"))
        .stdout(predicates::str::contains("paused"));

    substrate(&space)
        .args(["status", "lab"])
        .assert()
        .success()
        .stdout(predicates::str::contains(
            "turn: user-name (moderator — paused)",
        ));

    substrate(&space)
        .args(["read", "lab"])
        .assert()
        .success()
        .stdout(predicates::str::contains("hello room"))
        .stdout(predicates::str::contains("pass").not());
}

#[test]
fn init_seeds_identity_and_registers_space() {
    let space = TempDir::new().unwrap();
    let home = space.path().join(".home");
    std::fs::create_dir_all(&home).unwrap();
    std::fs::write(home.join("identity.yaml"), "name: user-name\n").unwrap();
    std::fs::write(
        home.join("participants.yaml"),
        "participants:\n  - name: claude-a\n    kind: agent\n",
    )
    .unwrap();

    substrate(&space)
        .arg("init")
        .assert()
        .success()
        .stdout(predicates::str::contains("claude-a"))
        .stdout(predicates::str::contains("user-name"));

    // the space is in the machine registry; spaces list/remove manage it
    let registry = std::fs::read_to_string(home.join("spaces.yaml")).unwrap();
    assert!(registry.contains(space.path().canonicalize().unwrap().to_str().unwrap()));

    let label_line = String::from_utf8(
        substrate(&space)
            .args(["spaces", "list"])
            .output()
            .unwrap()
            .stdout,
    )
    .unwrap();
    let label = label_line.split_whitespace().next().unwrap().to_string();
    substrate(&space)
        .args(["spaces", "remove", &label])
        .assert()
        .success();
    substrate(&space)
        .args(["spaces", "list"])
        .assert()
        .success()
        .stdout(predicates::str::contains("no spaces registered"));
}

#[test]
fn out_of_turn_write_fails() {
    let space = TempDir::new().unwrap();
    set_up_lab(&space);

    substrate(&space)
        .args(["write", "lab", "--as", "pat", "-m", "me first!"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("user-name"));
}

#[test]
fn unknown_participant_and_bad_names_fail() {
    let space = TempDir::new().unwrap();
    set_up_lab(&space);

    substrate(&space)
        .args(["write", "lab", "--as", "ghost", "-m", "boo"])
        .assert()
        .failure();
    substrate(&space)
        .args(["add", "Bad_Name", "--kind", "agent"])
        .assert()
        .failure();
    substrate(&space)
        .args(["add", "user-name", "--kind", "other"])
        .assert()
        .failure()
        .stderr(predicates::str::contains("already registered"));
}
