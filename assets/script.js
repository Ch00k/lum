const evtSource = new EventSource('/events?file=' + encodeURIComponent(filePath));
evtSource.onmessage = function (event) {
    if (event.data === 'reload') {
        location.reload();
    }
};

(function () {
    var container = document.querySelector('.container');
    var buttons = document.querySelectorAll('.width-switcher button');
    var stored = localStorage.getItem('lum-width');

    function setWidth(width) {
        if (width === '1200') {
            container.classList.add('w1200');
        } else {
            container.classList.remove('w1200');
        }
        localStorage.setItem('lum-width', width);
        buttons.forEach(function (btn) {
            btn.classList.toggle('active', btn.getAttribute('data-width') === width);
        });
    }

    setWidth(stored === '1200' ? '1200' : '900');

    buttons.forEach(function (btn) {
        btn.addEventListener('click', function () {
            setWidth(btn.getAttribute('data-width'));
        });
    });
})();
