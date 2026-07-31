function toggleMobileMenu() {
    document.querySelector('.nav-links').classList.toggle('open');
}

function scrollToSection(id) {
    var el = document.getElementById(id);
    if (el) el.scrollIntoView({ behavior: 'smooth' });
}

function showPricing(plan) {
    var el = document.getElementById('pricing');
    if (el) el.scrollIntoView({ behavior: 'smooth' });
    if (plan) {
        setTimeout(function() {
            var card = document.querySelector('.pricing-card[data-plan="' + plan + '"]');
            if (!card) {
                var cards = document.querySelectorAll('.pricing-card');
                if (plan === 'individual' && cards[0]) cards[0].style.borderColor = 'var(--primary)';
                if (plan === 'professional' && cards[1]) cards[1].style.borderColor = 'var(--primary)';
            }
        }, 600);
    }
}

function switchFeatureTab(tab) {
    document.querySelectorAll('.feature-tab').forEach(function(t) { t.classList.remove('active'); });
    document.querySelectorAll('.feature-tab[data-tab="' + tab + '"]').forEach(function(t) { t.classList.add('active'); });
    document.querySelectorAll('.feature-tab-content').forEach(function(c) { c.classList.remove('active'); });
    var target = document.getElementById('tab-' + tab);
    if (target) target.classList.add('active');
}

function switchToolCat(btn, cat) {
    document.querySelectorAll('.tool-cat').forEach(function(b) { b.classList.remove('active'); });
    btn.classList.add('active');
    document.querySelectorAll('.tool-grid').forEach(function(g) { g.style.display = 'none'; });
    var target = document.getElementById('tool-grid-' + cat);
    if (target) target.style.display = 'grid';
}

function showToast(msg) {
    var container = document.getElementById('toastContainer');
    var toast = document.createElement('div');
    toast.className = 'toast';
    toast.innerHTML = '<i class="fas fa-info-circle"></i> ' + msg;
    container.appendChild(toast);
    setTimeout(function() { toast.remove(); }, 3000);
}

function handleContactSubmit(event) {
    event.preventDefault();
    var form = event.target;
    var btn = form.querySelector('button[type="submit"]');
    var origText = btn.innerHTML;
    btn.innerHTML = '<i class="fas fa-spinner fa-spin"></i> Sending...';
    btn.disabled = true;

    var formData = new FormData(form);
    fetch(form.action, {
        method: 'POST',
        body: formData,
        headers: { 'Accept': 'application/json' }
    }).then(function(resp) {
        if (resp.ok) {
            form.style.display = 'none';
            var success = document.getElementById('formSuccess');
            if (success) success.style.display = 'block';
            if (success) success.scrollIntoView({ behavior: 'smooth' });
        } else {
            btn.innerHTML = '<i class="fas fa-exclamation-triangle"></i> Error';
            btn.disabled = false;
            setTimeout(function() { btn.innerHTML = origText; btn.disabled = false; }, 3000);
        }
    }).catch(function() {
        btn.innerHTML = '<i class="fas fa-exclamation-triangle"></i> Error';
        btn.disabled = false;
        setTimeout(function() { btn.innerHTML = origText; btn.disabled = false; }, 3000);
    });

    return false;
}

document.querySelectorAll('.nav-link').forEach(function(link) {
    link.addEventListener('click', function(e) {
        e.preventDefault();
        var target = this.getAttribute('href');
        document.querySelectorAll('.nav-link').forEach(function(l) { l.classList.remove('active'); });
        this.classList.add('active');
        if (target && target !== '#') scrollToSection(target.replace('#', ''));
        document.querySelector('.nav-links').classList.remove('open');
    });
});

var navSections = document.querySelectorAll('section[id]');
var navLinks = document.querySelectorAll('.nav-link[data-section]');

window.addEventListener('scroll', function() {
    var current = '';
    navSections.forEach(function(section) {
        var sectionTop = section.offsetTop - 100;
        if (window.pageYOffset >= sectionTop) {
            current = section.getAttribute('id');
        }
    });
    navLinks.forEach(function(link) {
        link.classList.remove('active');
        if (link.getAttribute('data-section') === current) {
            link.classList.add('active');
        }
    });
});