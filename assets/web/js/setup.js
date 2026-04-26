function showStatus(msg, isErr) {
    const el = document.getElementById('status');
    el.textContent = msg;
    el.className = 'status ' + (isErr ? 'err' : 'ok');
}

document.addEventListener('keydown', e => {
    if (e.key === 'Enter') submitSetup();
});

async function submitSetup() {
    const username = document.getElementById('f-username').value.trim();
    const password = document.getElementById('f-password').value;
    const confirm = document.getElementById('f-confirm').value;
    const btn = document.getElementById('submit-btn');

    if (!username) return showStatus('Username cannot be empty.', true);
    if (!password) return showStatus('Password cannot be empty.', true);
    if (password !== confirm) return showStatus('Passwords do not match.', true);

    btn.disabled = true;

    try {
        const res = await fetch('/setup/api/init', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ new_username: username, new_password: password }),
        });

        if (res.ok) {
            showStatus('Redirecting…', false);
            setTimeout(() => { window.location.href = '/admin'; }, 800);
        } else {
            showStatus((await res.text()) || 'Something went wrong.', true);
            btn.disabled = false;
        }
    } catch {
        showStatus('Request failed.', true);
        btn.disabled = false;
    }
}