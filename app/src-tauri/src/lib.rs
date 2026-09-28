use std::path::PathBuf;

use tauri_plugin_window_state::StateFlags;
use std::process::Command;

use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem, Submenu},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter, Manager, WindowEvent,
};

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
struct DaemonConfig {
    addr: Option<String>,
    token: Option<String>,
    default_addr: &'static str,
}

fn bench_home() -> PathBuf {
    // Filter empty to match Go's `h != ""` in daemon/internal/paths
    match std::env::var_os("BENCH_HOME").filter(|h| !h.is_empty()) {
        Some(home) => PathBuf::from(home),
        None => std::env::home_dir().unwrap_or_default().join(".bench"),
    }
}

fn read_trimmed(path: PathBuf) -> Option<String> {
    let contents = std::fs::read_to_string(path).ok()?;
    let trimmed = contents.trim();
    (!trimmed.is_empty()).then(|| trimmed.to_string())
}

#[tauri::command]
fn daemon_config() -> DaemonConfig {
    let home = bench_home();
    DaemonConfig {
        addr: read_trimmed(home.join("benchd.addr")),
        token: read_trimmed(home.join("token")),
        default_addr: "127.0.0.1:23624",
    }
}

/// A site in the tray's Sites submenu.
#[derive(serde::Deserialize)]
struct TraySite {
    name: String,
    host: String,
}

/// tray_menu builds the tray menu: a status line (disabled, it's only
/// read), Open, a Sites submenu, Activity, Stop and Quit (audit UI-09).
fn tray_menu(
    app: &tauri::AppHandle,
    status: &str,
    sites: &[TraySite],
) -> tauri::Result<Menu<tauri::Wry>> {
    let status = MenuItem::with_id(app, "status", status, false, None::<&str>)?;
    let open = MenuItem::with_id(app, "open", "Open Bench", true, None::<&str>)?;
    let site_items = sites
        .iter()
        .map(|s| MenuItem::with_id(app, format!("site:{}", s.name), &s.host, true, None::<&str>))
        .collect::<tauri::Result<Vec<_>>>()?;
    let site_refs: Vec<&dyn tauri::menu::IsMenuItem<tauri::Wry>> =
        site_items.iter().map(|i| i as &dyn tauri::menu::IsMenuItem<tauri::Wry>).collect();
    let sites_menu = Submenu::with_items(app, "Sites", !sites.is_empty(), &site_refs)?;
    let activity = MenuItem::with_id(app, "activity", "Activity", true, None::<&str>)?;
    let stop = MenuItem::with_id(app, "stop", "Stop Bench…", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit Bench (sites keep running)", true, None::<&str>)?;
    Menu::with_items(
        app,
        &[
            &status,
            &PredefinedMenuItem::separator(app)?,
            &open,
            &sites_menu,
            &activity,
            &PredefinedMenuItem::separator(app)?,
            &stop,
            &PredefinedMenuItem::separator(app)?,
            &quit,
        ],
    )
}

/// update_tray refreshes the tray menu with Bench's status and sites; the
/// window calls it whenever either changes.
#[tauri::command]
fn update_tray(app: tauri::AppHandle, status: String, sites: Vec<TraySite>) -> Result<(), String> {
    let menu = tray_menu(&app, &status, &sites).map_err(|e| e.to_string())?;
    let tray = app.tray_by_id("main-tray").ok_or("the tray icon isn't there")?;
    tray.set_menu(Some(menu)).map_err(|e| e.to_string())?;
    tray.set_tooltip(Some(&status)).map_err(|e| e.to_string())
}

/// The folder benchd writes its log to, for "Open logs folder".
#[tauri::command]
fn logs_dir() -> String {
    bench_home().join("logs").to_string_lossy().into_owned()
}

#[tauri::command]
fn start_daemon() -> Result<(), String> {
    let name = if cfg!(windows) { "benchd.exe" } else { "benchd" };
    let sibling = std::env::current_exe()
        .ok()
        .and_then(|exe| exe.parent().map(|dir| dir.join(name)));
    let program = match sibling {
        Some(p) if p.exists() => p,
        _ => PathBuf::from(name), // falls back to PATH lookup in spawn
    };

    let mut cmd = Command::new(&program);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        // CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS
        cmd.creation_flags(0x0000_0200 | 0x0000_0008);
    }
    // Drop the Child without waiting: the daemon must outlive the GUI
    cmd.spawn()
        .map(|_| ())
        .map_err(|e| format!("failed to start benchd ({}): {}", program.display(), e))
}

fn show_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

/// Passed by the OS autostart entry: start in the tray, not on screen.
const HIDDEN_ARG: &str = "--hidden";

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        // First, so a second launch focuses this window instead of starting
        // another app that would race this one to start the daemon.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            show_main_window(app)
        }))
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_autostart::Builder::new().arg(HIDDEN_ARG).build())
        // Remember size and position, but not visibility: a sign-in launch
        // must stay in the tray whatever the last session looked like.
        .plugin(
            tauri_plugin_window_state::Builder::new()
                .with_state_flags(StateFlags::all() & !StateFlags::VISIBLE)
                .build(),
        )
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_dialog::init())
        .invoke_handler(tauri::generate_handler![daemon_config, start_daemon, logs_dir, update_tray])
        // Closing the window keeps Bench in the tray (sites keep being
        // served either way); Quit lives in the tray menu.
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .setup(|app| {
            let menu = tray_menu(app.handle(), "Bench: starting", &[])?;

            let _tray = TrayIconBuilder::with_id("main-tray")
                .icon(app.default_window_icon().unwrap().clone())
                .tooltip("Bench")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "open" => show_main_window(app),
                    // The window handles these (navigation, the stop
                    // confirmation), so it comes forward first.
                    id @ ("activity" | "stop") => {
                        show_main_window(app);
                        let _ = app.emit("tray", id);
                    }
                    // "site:<name>" opens that site's page.
                    id if id.starts_with("site:") => {
                        show_main_window(app);
                        let _ = app.emit("tray", id);
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        show_main_window(tray.app_handle());
                    }
                })
                .build(app)?;

            // The window starts hidden (tauri.conf.json) so a sign-in launch
            // stays in the tray; any other launch shows it.
            if !std::env::args().any(|a| a == HIDDEN_ARG) {
                show_main_window(app.handle());
            }
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|_app, _event| {
        // macOS: clicking the Dock icon brings a hidden window back.
        #[cfg(target_os = "macos")]
        if let tauri::RunEvent::Reopen { .. } = _event {
            show_main_window(_app);
        }
    });
}
