//! Slash-command grammar for the input box. Moderator powers, plus /pass
//! and /help for everyone.

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SlashCommand {
    Topic(String),
    Turns(Vec<String>),
    Quiet { name: String, turns: u32 },
    Unquiet(String),
    Invite(String),
    Next(String),
    Pass,
    End,
    Resume,
    Help,
}

impl SlashCommand {
    /// Commands any participant may use; the rest are moderator-only.
    pub fn anyone_may(&self) -> bool {
        matches!(self, SlashCommand::Pass | SlashCommand::Help)
    }
}

pub const HELP: &str = "/topic <text> · /turns <name> <name>… · /quiet <name> [n] · /unquiet <name> · /invite <name> · /next <name> · /pass · /end · /resume · /help";

pub fn parse(input: &str) -> Result<SlashCommand, String> {
    let input = input.trim();
    let mut words = input.split_whitespace();
    let command = words.next().unwrap_or("");
    let rest: Vec<&str> = words.collect();

    match command {
        "/topic" => {
            let topic = input["/topic".len()..].trim();
            if topic.is_empty() {
                return Err("usage: /topic <new topic>".into());
            }
            Ok(SlashCommand::Topic(topic.to_string()))
        }
        "/turns" => {
            if rest.is_empty() {
                return Err("usage: /turns <name> <name>…".into());
            }
            Ok(SlashCommand::Turns(
                rest.iter().map(|s| s.to_string()).collect(),
            ))
        }
        "/quiet" => match rest.as_slice() {
            [name] => Ok(SlashCommand::Quiet {
                name: name.to_string(),
                turns: 1,
            }),
            [name, n] => n
                .parse::<u32>()
                .map_err(|_| format!("'{n}' is not a number of turns"))
                .map(|turns| SlashCommand::Quiet {
                    name: name.to_string(),
                    turns,
                }),
            _ => Err("usage: /quiet <name> [turns]".into()),
        },
        "/unquiet" => match rest.as_slice() {
            [name] => Ok(SlashCommand::Unquiet(name.to_string())),
            _ => Err("usage: /unquiet <name>".into()),
        },
        "/invite" => match rest.as_slice() {
            [name] => Ok(SlashCommand::Invite(name.to_string())),
            _ => Err("usage: /invite <name>".into()),
        },
        "/next" => match rest.as_slice() {
            [name] => Ok(SlashCommand::Next(name.to_string())),
            _ => Err("usage: /next <name>".into()),
        },
        "/pass" if rest.is_empty() => Ok(SlashCommand::Pass),
        "/end" if rest.is_empty() => Ok(SlashCommand::End),
        "/resume" if rest.is_empty() => Ok(SlashCommand::Resume),
        "/help" if rest.is_empty() => Ok(SlashCommand::Help),
        other => Err(format!("unknown command '{other}' — {HELP}")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_the_grammar() {
        assert_eq!(
            parse("/topic  storage layer  "),
            Ok(SlashCommand::Topic("storage layer".into()))
        );
        assert_eq!(
            parse("/turns pat claude-a"),
            Ok(SlashCommand::Turns(vec!["pat".into(), "claude-a".into()]))
        );
        assert_eq!(
            parse("/quiet codex-b"),
            Ok(SlashCommand::Quiet {
                name: "codex-b".into(),
                turns: 1
            })
        );
        assert_eq!(
            parse("/quiet codex-b 3"),
            Ok(SlashCommand::Quiet {
                name: "codex-b".into(),
                turns: 3
            })
        );
        assert_eq!(
            parse("/unquiet codex-b"),
            Ok(SlashCommand::Unquiet("codex-b".into()))
        );
        assert_eq!(
            parse("/invite gemini-c"),
            Ok(SlashCommand::Invite("gemini-c".into()))
        );
        assert_eq!(parse("/next codex"), Ok(SlashCommand::Next("codex".into())));
        assert!(parse("/next").is_err());
        assert!(!parse("/next codex").unwrap().anyone_may());
        assert_eq!(parse("/pass"), Ok(SlashCommand::Pass));
        assert_eq!(parse("/end"), Ok(SlashCommand::End));
        assert_eq!(parse("/resume"), Ok(SlashCommand::Resume));
        assert!(parse("/resume now").is_err());
        assert_eq!(parse("/help"), Ok(SlashCommand::Help));
    }

    #[test]
    fn rejects_bad_input() {
        assert!(parse("/topic").is_err());
        assert!(parse("/quiet").is_err());
        assert!(parse("/quiet a b c").is_err());
        assert!(parse("/quiet a notanumber").is_err());
        assert!(parse("/banana").is_err());
        assert!(parse("/pass now").is_err());
    }

    #[test]
    fn permissions() {
        assert!(parse("/pass").unwrap().anyone_may());
        assert!(parse("/help").unwrap().anyone_may());
        assert!(!parse("/end").unwrap().anyone_may());
        assert!(!parse("/topic x").unwrap().anyone_may());
    }
}
