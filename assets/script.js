const evtSource = new EventSource('/events?file=' + encodeURIComponent(filePath));
evtSource.onmessage = function (event) {
    if (event.data === 'reload') {
        location.reload();
    }
};
