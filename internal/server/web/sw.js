self.addEventListener('install', (event) => {
    event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
    event.waitUntil(self.clients.claim());
});

self.addEventListener('push', (event) => {
    let data; // Define data in scope
    if (event.data) {
        try {
            data = event.data.json();
        } catch (e) {
            data = {
                title: 'Notification',
                body: event.data.text(),
                data: { url: '/' }
            };
        }
    }

    const options = {
        body: data.body,
        icon: '/images/purple-skull-512-maskable-square.png',
        badge: '/images/purple-skull-512-maskable-square.png',
        data: data.data,
        vibrate: [200, 100, 200],
        actions: [
            {
                action: 'checkin',
                title: '✅ Check In'
            }
        ]
    };

    event.waitUntil(
        self.registration.showNotification(data.title, options)
    );
});

self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const switchId = event.notification.data?.id;

    if (event.action === 'checkin' && switchId) {
        event.waitUntil(
            // The API change doesn't break this endpoint, 
            // but we call it to move status from 'active' back to a full timer
            fetch(`/api/v1/switch/${switchId}/reset`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
                // Note: We don't send the pushSubscription here because 
                // the SW doesn't have easy access to it, and the server 
                // should retain the existing one if not provided.
            }).then(response => {
                if (response.ok) {
                    return self.registration.showNotification('Checked In', {
                        body: 'The switch timer has been reset.',
                        icon: '/images/purple-skull-512-maskable-square.png',
                        tag: 'reset-success'
                    });
                }
            }).catch(err => console.error("Check-in fetch failed:", err))
        );
    } 
    else {
        event.waitUntil(
            clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
                for (const client of clientList) {
                    if (client.url === '/' && 'focus' in client) return client.focus();
                }
                if (clients.openWindow) return clients.openWindow(event.notification.data?.url || '/');
            })
        );
    }
});