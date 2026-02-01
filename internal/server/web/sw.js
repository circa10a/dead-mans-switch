self.addEventListener('install', (event) => {
    event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
    event.waitUntil(self.clients.claim());
});
self.addEventListener('push', (event) => {
    if (event.data) {
        try {
            // Try to parse as JSON
            data = event.data.json();
        } catch (e) {
            // If it's not JSON (like your DevTools test), use the raw text as the body
            data = {
                title: 'Notification',
                body: event.data.text(),
                data: { url: '/' }
            };
        }
    }

    const options = {
        body: data.body,
        icon: 'https://cdn-icons-png.flaticon.com/512/15151/15151343.png',
        badge: 'https://cdn-icons-png.flaticon.com/512/15151/15151343.png',
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

// Merged single listener for all clicks
self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const switchId = event.notification.data?.id;

    // Handle the "Check In" button specifically
    if (event.action === 'checkin' && switchId) {
        event.waitUntil(

            fetch(`/api/v1/switch/${switchId}/reset`, {
                method: 'POST'
            }).then(response => {
                if (response.ok) {
                    return self.registration.showNotification('Switch Reset', {
                        body: 'Checked In Successfully',
                        icon: '/icon.png',
                        tag: 'reset-success' // Overwrites previous alerts
                    });
                }
            }).catch(err => console.error("Check-in fetch failed:", err))
        );
    } 
    // Handle tapping the notification body
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
