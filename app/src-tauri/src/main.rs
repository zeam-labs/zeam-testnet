

#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

use std::process::{Child, Command};
use std::sync::Mutex;
use tauri::{Manager, State};


struct ZeamProcess {
    backend: Mutex<Option<Child>>,
}


fn find_free_port(start: u16) -> u16 {
    use std::net::TcpListener;
    for port in start..start + 100 {
        if TcpListener::bind(("127.0.0.1", port)).is_ok() {
            return port;
        }
    }
    start
}


fn start_backend(app_handle: &tauri::AppHandle) -> Result<(Child, u16), String> {
    let port = find_free_port(19840);

    
    let resource_dir = app_handle
        .path()
        .resource_dir()
        .map_err(|e| format!("Failed to get resource dir: {}", e))?;

    
    #[cfg(target_os = "windows")]
    let backend_name = "zeam-backend.exe";
    #[cfg(not(target_os = "windows"))]
    let backend_name = "zeam-backend";

    let sidecar_path = resource_dir.join("binaries").join(backend_name);
    let sidecar_path = if sidecar_path.exists() {
        sidecar_path
    } else {
        
        let flat_path = resource_dir.join(backend_name);
        if flat_path.exists() {
            flat_path
        } else {
            
            std::env::current_exe()
                .map(|p| p.parent().unwrap().join(backend_name))
                .map_err(|e| format!("Failed to get exe dir: {}", e))?
        }
    };

    
    let oewn_path = resource_dir.join("resources/oewn.json");
    let oewn_path = if oewn_path.exists() {
        oewn_path
    } else {
        
        std::env::current_dir()
            .map(|p| p.join("../../oewn.json"))
            .map_err(|e| format!("Failed to get current dir: {}", e))?
    };

    
    let data_dir = app_handle
        .path()
        .app_data_dir()
        .unwrap_or_else(|_| std::path::PathBuf::from(".zeam"));

    println!("[ZEAM App] Starting backend on port {}", port);
    println!("[ZEAM App] Binary: {:?}", sidecar_path);
    println!("[ZEAM App] OEWN: {:?}", oewn_path);
    println!("[ZEAM App] Data: {:?}", data_dir);

    
    let child = Command::new(&sidecar_path)
        .arg("-port")
        .arg(port.to_string())
        .arg("-oewn")
        .arg(&oewn_path)
        .arg("-data")
        .arg(&data_dir)
        .spawn()
        .map_err(|e| format!("Failed to start backend: {}", e))?;

    Ok((child, port))
}


#[tauri::command]
fn get_backend_url(port: State<'_, u16>) -> String {
    format!("http://127.0.0.1:{}", *port)
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_process::init())
        .setup(|app| {
            // Start the NGAC backend (includes L2 block monitor)
            let (backend_child, port) = start_backend(&app.handle())
                .expect("Failed to start ZEAM backend");

            // Store process handle for cleanup
            app.manage(ZeamProcess {
                backend: Mutex::new(Some(backend_child)),
            });
            app.manage(port);

            // Wait for backend to be ready
            let addr = format!("127.0.0.1:{}", port);
            println!("[ZEAM App] Waiting for backend at {}", addr);
            for i in 0..30 {
                if std::net::TcpStream::connect(&addr).is_ok() {
                    println!("[ZEAM App] Backend ready after {}ms", i * 500);
                    break;
                }
                std::thread::sleep(std::time::Duration::from_millis(500));
            }

            // Update the window to point to the backend
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.eval(&format!(
                    "window.ZEAM_BACKEND_URL = 'http:
                    port
                ));
            }

            println!("[ZEAM App] ZEAM is ready!");
            println!("[ZEAM App] L2 block monitor connecting to Base Sepolia + OP Sepolia");

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { .. } = event {
                
                if let Some(process) = window.try_state::<ZeamProcess>() {
                    if let Ok(mut guard) = process.backend.lock() {
                        if let Some(mut child) = guard.take() {
                            println!("[ZEAM App] Stopping backend...");
                            let _ = child.kill();
                        }
                    }
                }
            }
        })
        .invoke_handler(tauri::generate_handler![get_backend_url])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
