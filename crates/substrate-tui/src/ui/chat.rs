//! Transcript rendering: wrap lines ourselves (textwrap) so the scroll math
//! is exact, then slice the wrapped lines by the scroll offset.

use ratatui::prelude::*;
use ratatui::widgets::Paragraph;

use crate::app::App;
use crate::ui::kind_color;

pub fn render(frame: &mut Frame, app: &mut App, area: Rect) {
    app.viewport_height = area.height as usize;
    let Some(view) = &mut app.view else { return };

    let width = area.width.saturating_sub(1).max(20) as usize;
    let mut lines: Vec<Line> = Vec::new();
    for entry in &view.entries {
        let color = kind_color(app.kinds.get(&entry.meta.author).copied());
        let mut header_style = Style::default().fg(color).add_modifier(Modifier::BOLD);
        if entry.meta.author == app.me {
            header_style = header_style.add_modifier(Modifier::UNDERLINED);
        }
        lines.push(Line::from(vec![
            Span::styled(entry.meta.author.to_string(), header_style),
            Span::styled(
                format!("  {}", entry.meta.timestamp.format("%Y-%m-%d %H:%M:%S UTC")),
                Style::default().fg(Color::DarkGray),
            ),
        ]));
        for body_line in entry.body.lines() {
            if body_line.is_empty() {
                lines.push(Line::raw(""));
            } else {
                for wrapped in textwrap::wrap(body_line, width) {
                    lines.push(Line::raw(wrapped.into_owned()));
                }
            }
        }
        lines.push(Line::raw(""));
    }

    let total = lines.len();
    let height = area.height as usize;
    let max_scroll = total.saturating_sub(height);
    if view.stick_bottom || view.scroll >= max_scroll {
        view.scroll = max_scroll;
        view.stick_bottom = true;
    }
    let visible: Vec<Line> = lines.into_iter().skip(view.scroll).take(height).collect();
    frame.render_widget(Paragraph::new(visible), area);
}
