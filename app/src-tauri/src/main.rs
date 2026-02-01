#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::process::{Child, Command};
use std::sync::Mutex;
use tauri::Manager;

const DAEMON_PORT: u16 = 19840;

struct DaemonProcess(Mutex<Option<Child>>);

#[tauri::command]
fn get_backend_url() -> String {
    format!("http://127.0.0.1:{}", DAEMON_PORT)
}

fn find_daemon_binary(app: &tauri::AppHandle) -> Option<std::path::PathBuf> {

    let exe = std::env::current_exe().ok()?;
    let dir = exe.parent()?;

    let names: &[&str] = if cfg!(target_os = "windows") {
        &["zeam-daemon.exe", "zeam-daemon-x86_64-pc-windows-msvc.exe", "zeam-daemon-x86_64-pc-windows-gnu.exe"]
    } else if cfg!(target_os = "macos") {
        &["zeam-daemon", "zeam-daemon-aarch64-apple-darwin", "zeam-daemon-x86_64-apple-darwin"]
    } else {
        &["zeam-daemon", "zeam-daemon-x86_64-unknown-linux-gnu"]
    };

    for name in names {
        let candidate = dir.join(name);
        if candidate.exists() {
            return Some(candidate);
        }
    }

    if let Ok(resource_dir) = app.path().resource_dir() {
        for name in names {
            let candidate = resource_dir.join(name);
            if candidate.exists() {
                return Some(candidate);
            }
        }
    }

    None
}

fn spawn_daemon(app: &tauri::AppHandle) -> Option<Child> {
    let binary = find_daemon_binary(app)?;

    eprintln!("[ZEAM] Spawning daemon: {:?} --port {}", binary, DAEMON_PORT);

    let child = Command::new(&binary)
        .arg("--port")
        .arg(DAEMON_PORT.to_string())
        .spawn();

    match child {
        Ok(child) => {
            eprintln!("[ZEAM] Daemon PID: {}", child.id());
            Some(child)
        }
        Err(e) => {
            eprintln!("[ZEAM] Failed to spawn daemon: {}", e);
            None
        }
    }
}

fn wait_for_daemon() -> bool {
    let _url = format!("http://127.0.0.1:{}/health", DAEMON_PORT);
    for i in 0..60 {
        if std::net::TcpStream::connect(format!("127.0.0.1:{}", DAEMON_PORT)).is_ok() {
            eprintln!("[ZEAM] Daemon ready after {}ms", i * 500);
            return true;
        }
        std::thread::sleep(std::time::Duration::from_millis(500));
    }
    eprintln!("[ZEAM] Daemon did not start within 30s");
    false
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(DaemonProcess(Mutex::new(None)))
        .invoke_handler(tauri::generate_handler![get_backend_url])
        .setup(|app| {
            let handle = app.handle();

            if let Some(child) = spawn_daemon(handle) {
                let state = handle.state::<DaemonProcess>();
                *state.0.lock().unwrap() = Some(child);

                if !wait_for_daemon() {
                    eprintln!("[ZEAM] Warning: Daemon may not be ready");
                }
            } else {
                eprintln!("[ZEAM] Error: Could not find or spawn daemon binary");
            }

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::Destroyed = event {
                let app = window.app_handle();
                let state = app.state::<DaemonProcess>();
                let mut guard = state.0.lock().unwrap();
                if let Some(ref mut child) = *guard {
                    eprintln!("[ZEAM] Shutting down daemon...");
                    let _ = child.kill();
                    let _ = child.wait();
                }
                *guard = None;
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running ZEAM");
}
