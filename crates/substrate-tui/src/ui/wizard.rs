//! First-run screens: confirm making the directory a space, and the
//! once-ever "who are you?" question.

use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, Clear, Paragraph, Wrap};
use tui_textarea::TextArea;

fn centered(area: Rect, width: u16, height: u16) -> Rect {
    let x = area.x + area.width.saturating_sub(width) / 2;
    let y = area.y + area.height.saturating_sub(height) / 2;
    Rect {
        x,
        y,
        width: width.min(area.width),
        height: height.min(area.height),
    }
}

pub fn draw_confirm_create(frame: &mut Frame, path: &std::path::Path) {
    let area = centered(frame.area(), 64, 8);
    frame.render_widget(Clear, area);
    let text = vec![
        Line::raw(""),
        Line::from(vec![
            Span::raw("  "),
            Span::styled(
                path.display().to_string(),
                Style::default().add_modifier(Modifier::BOLD),
            ),
            Span::raw(" is not a substrate space yet."),
        ]),
        Line::raw(""),
        Line::raw("  Make it one? Threads will live in this directory."),
        Line::raw(""),
        Line::from(Span::styled(
            "  y / enter: create        n / esc: quit",
            Style::default().fg(Color::DarkGray),
        )),
    ];
    frame.render_widget(
        Paragraph::new(text)
            .wrap(Wrap { trim: false })
            .block(Block::default().borders(Borders::ALL).title(" substrate ")),
        area,
    );
}

pub fn draw_ask_name(frame: &mut Frame, input: &TextArea<'static>, error: Option<&str>) {
    let area = centered(frame.area(), 64, 9);
    frame.render_widget(Clear, area);
    let block = Block::default().borders(Borders::ALL).title(" substrate ");
    let inner = block.inner(area);
    frame.render_widget(block, area);

    let rows = Layout::vertical([
        Constraint::Length(2),
        Constraint::Length(3),
        Constraint::Length(2),
    ])
    .split(inner);
    frame.render_widget(
        Paragraph::new("  Who are you? (asked once — lowercase a-z0-9-)"),
        rows[0],
    );
    let mut input = input.clone();
    input.set_block(Block::default().borders(Borders::ALL));
    frame.render_widget(&input, rows[1]);
    let hint = match error {
        Some(e) => Line::from(Span::styled(
            format!("  {e}"),
            Style::default().fg(Color::Yellow),
        )),
        None => Line::from(Span::styled(
            "  enter: continue        esc: quit",
            Style::default().fg(Color::DarkGray),
        )),
    };
    frame.render_widget(Paragraph::new(hint), rows[2]);
}
