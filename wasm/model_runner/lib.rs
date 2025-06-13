use std::slice;
use std::str;

static mut RESULT_PTR: *const u8 = 0 as *const u8;
static mut RESULT_LEN: usize = 0;

#[no_mangle]
pub extern "C" fn allocate(size: usize) -> *mut u8 {
    let mut buf = Vec::with_capacity(size);
    let ptr = buf.as_mut_ptr();
    std::mem::forget(buf);
    ptr
}

#[no_mangle]
pub extern "C" fn run_mistral(
    model_ptr: *const u8,
    model_len: usize,
    prompt_ptr: *const u8,
    prompt_len: usize,
) -> *const u8 {
    let model = unsafe { slice::from_raw_parts(model_ptr, model_len) };
    let prompt = unsafe { slice::from_raw_parts(prompt_ptr, prompt_len) };

    let prompt_str = match str::from_utf8(prompt) {
        Ok(s) => s,
        Err(_) => "",
    };

    // Hash prompt deterministically — no string checks, no external logic
    let seed = 5381u64;
    let mut hash = seed;
    for byte in prompt_str.bytes() {
        hash = (hash.wrapping_shl(5)).wrapping_add(hash) ^ byte as u64;
    }

    let minimal_response = match hash % 3 {
        0 => "Reflex acknowledged from sealed memory.",
        1 => "Memory tension elevated. Standing by.",
        _ => "No emergent pattern detected.",
    };

    let result_bytes = minimal_response.as_bytes();

    unsafe {
        RESULT_PTR = result_bytes.as_ptr();
        RESULT_LEN = result_bytes.len();
        RESULT_PTR
    }
}

#[no_mangle]
pub extern "C" fn response_len() -> usize {
    unsafe { RESULT_LEN }
}
