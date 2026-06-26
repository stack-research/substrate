use ratatui::prelude::*;

use crate::app::App;
pub fn render(frame: &mut Frame, app: &App, area: Rect) {
    let Some(view) = &app.view else { return };

    let line = if let Some((message, _)) = &app.flash {
        Line::from(Span::styled(
            format!(" {message}"),
            Style::default().fg(Color::Yellow),
        ))
    } else if view.config.status == substrate_core::ThreadStatus::Ended {
        Line::from(Span::styled(" ended", Style::default().fg(Color::DarkGray)))
    } else if app.is_moderator_of_open_thread() && view.config.current() != &app.me {
        Line::from(vec![
            Span::styled(" moderator", Style::default().fg(Color::Green)),
            Span::styled(
                format!(" · current: {}", view.config.current()),
                Style::default().fg(Color::DarkGray),
            ),
        ])
    } else if app.is_moderator_of_open_thread() && view.config.is_paused() {
        Line::from(vec![
            Span::styled(" moderator", Style::default().fg(Color::Green)),
            Span::styled(" · paused on you", Style::default().fg(Color::DarkGray)),
        ])
    } else if view.config.current() == &app.me {
        Line::from(Span::styled(
            " your turn",
            Style::default().fg(Color::Green),
        ))
    } else {
        Line::from(Span::styled(
            format!(" waiting for {} ", view.config.current()),
            Style::default().fg(Color::DarkGray),
        ))
    };
    frame.render_widget(line, area);
}
