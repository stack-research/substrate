use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, List, ListItem, ListState};
use substrate_core::ThreadStatus;

use crate::app::App;
use crate::ui::kind_color;

pub fn render(frame: &mut Frame, app: &mut App) {
    let layout = Layout::vertical([
        Constraint::Length(1),
        Constraint::Min(1),
        Constraint::Length(1),
    ])
    .split(frame.area());

    let me_color = kind_color(app.kinds.get(&app.me).copied());
    let header = Line::from(vec![
        Span::styled(" substrate ", Style::default().add_modifier(Modifier::BOLD)),
        Span::raw("· you are "),
        Span::styled(app.me.to_string(), Style::default().fg(me_color)),
        Span::raw(format!(" · {}", app.space.root().display())),
    ]);
    frame.render_widget(header, layout[0]);

    let items: Vec<ListItem> = if app.summaries.is_empty() {
        vec![ListItem::new(
            "no threads yet — create one with `substrate new`",
        )]
    } else {
        app.summaries
            .iter()
            .map(|s| {
                let yours = s.current == app.me;
                let mut spans = vec![
                    Span::styled(
                        format!("{:<20}", s.thread),
                        Style::default().add_modifier(Modifier::BOLD),
                    ),
                    if s.status == ThreadStatus::Ended {
                        Span::styled("ended   ", Style::default().fg(Color::DarkGray))
                    } else if yours {
                        Span::styled("your turn", Style::default().fg(Color::Green))
                    } else {
                        Span::styled(
                            format!("turn: {}", s.current),
                            Style::default().fg(Color::Gray),
                        )
                    },
                ];
                if s.paused && !yours {
                    spans.push(Span::styled(
                        " (paused)",
                        Style::default().fg(Color::DarkGray),
                    ));
                }
                spans.push(Span::styled(
                    format!("  — {}", s.topic),
                    Style::default().fg(Color::DarkGray),
                ));
                ListItem::new(Line::from(spans))
            })
            .collect()
    };

    let list = List::new(items)
        .block(Block::default().borders(Borders::ALL).title(" threads "))
        .highlight_style(Style::default().add_modifier(Modifier::REVERSED));
    let mut state = ListState::default();
    state.select(Some(app.list_index));
    frame.render_stateful_widget(list, layout[1], &mut state);

    let hint = match &app.flash {
        Some((message, _)) => Line::from(Span::styled(
            format!(" {message}"),
            Style::default().fg(Color::Yellow),
        )),
        None => Line::from(Span::styled(
            " ↑/↓ select · enter open · r refresh · q quit",
            Style::default().fg(Color::DarkGray),
        )),
    };
    frame.render_widget(hint, layout[2]);
}
