//! The `n` form: create a thread without leaving the TUI.

use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, Clear, Paragraph};

use crate::app::{App, FORM_FIELDS};

pub fn render(frame: &mut Frame, app: &App, area: Rect) {
    let Some(form) = &app.form else { return };
    let width = area.width.saturating_sub(8).clamp(40, 76);
    let height = 15;
    let x = area.x + area.width.saturating_sub(width) / 2;
    let y = area.y + area.height.saturating_sub(height) / 2;
    let popup = Rect {
        x,
        y,
        width,
        height: height.min(area.height),
    };
    frame.render_widget(Clear, popup);
    let block = Block::default().borders(Borders::ALL).title(" new thread ");
    let inner = block.inner(popup);
    frame.render_widget(block, popup);

    let rows = Layout::vertical([
        Constraint::Length(3),
        Constraint::Length(3),
        Constraint::Length(3),
        Constraint::Length(2),
        Constraint::Min(0),
    ])
    .split(inner);

    let labels = [
        "name",
        "topic",
        "turns (speaking order — you open and moderate)",
    ];
    for (i, label) in labels.iter().enumerate() {
        let mut input = form.fields[i].clone();
        let style = if form.focus == i {
            Style::default().fg(Color::Green)
        } else {
            Style::default().fg(Color::DarkGray)
        };
        input.set_block(
            Block::default()
                .borders(Borders::ALL)
                .border_style(style)
                .title(format!(" {label} ")),
        );
        input.set_cursor_line_style(Style::default());
        frame.render_widget(&input, rows[i]);
    }
    debug_assert_eq!(labels.len(), FORM_FIELDS);

    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(
            " tab: next field · enter: create · esc: cancel",
            Style::default().fg(Color::DarkGray),
        ))),
        rows[3],
    );
}
