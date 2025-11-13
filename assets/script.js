const evtSource = new EventSource('/events');
evtSource.onmessage = function (event) {
    if (event.data === 'reload') {
        location.reload();
    }
};
