mod chat;
mod form;
mod input;
mod list;
mod status;
pub mod wizard;

use ratatui::prelude::*;

use crate::app::{App, Mode};

pub fn render(frame: &mut Frame, app: &mut App) {
    match app.mode {
        Mode::List => list::render(frame, app),
        Mode::Thread => render_thread(frame, app),
        Mode::NewThread => {
            list::render(frame, app);
            form::render(frame, app, frame.area());
        }
    }
}

fn render_thread(frame: &mut Frame, app: &mut App) {
    let layout = Layout::vertical([
        Constraint::Length(1), // header
        Constraint::Min(1),    // transcript
        Constraint::Length(5), // input
        Constraint::Length(1), // status
    ])
    .split(frame.area());

    header(frame, app, layout[0]);
    chat::render(frame, app, layout[1]);
    input::render(frame, app, layout[2]);
    status::render(frame, app, layout[3]);
}

fn header(frame: &mut Frame, app: &App, area: Rect) {
    let Some(view) = &app.view else { return };
    let line = Line::from(vec![
        Span::styled(
            format!(" {} ", view.name),
            Style::default().add_modifier(Modifier::BOLD),
        ),
        Span::raw("· "),
        Span::styled(&view.config.topic, Style::default().fg(Color::Gray)),
    ]);
    frame.render_widget(line, area);
}

pub(crate) fn kind_color(kind: Option<substrate_core::ParticipantKind>) -> Color {
    use substrate_core::ParticipantKind::*;
    match kind {
        Some(Human) => Color::Cyan,
        Some(Agent) => Color::Magenta,
        Some(Other) | None => Color::Yellow,
    }
}
