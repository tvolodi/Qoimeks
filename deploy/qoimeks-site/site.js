(function () {
  var root = document.documentElement;
  root.className = "js";

  function setLang(l) {
    root.setAttribute("data-lang", l);
    root.lang = l;
    var b = document.querySelectorAll(".lang button");
    for (var i = 0; i < b.length; i++) {
      b[i].setAttribute("aria-pressed", b[i].getAttribute("data-set") === l ? "true" : "false");
    }
    try { localStorage.setItem("lang", l); } catch (e) {}
  }
  var saved = null;
  try { saved = localStorage.getItem("lang"); } catch (e) {}
  setLang(saved || ((navigator.language || "ru").slice(0, 2) === "kk" ? "kk" : "ru"));
  var buttons = document.querySelectorAll(".lang button");
  for (var i = 0; i < buttons.length; i++) {
    buttons[i].addEventListener("click", function () { setLang(this.getAttribute("data-set")); });
  }

  // Fills version, notes and the download link from latest.json.
  // On any failure the fallback message stays visible and the install steps still work.
  function show(m) {
    document.getElementById("version").textContent = m.versionName;
    document.getElementById("published").textContent = m.published || "";
    document.getElementById("notes-ru").textContent = (m.notes && m.notes.ru) || "";
    document.getElementById("notes-kk").textContent = (m.notes && m.notes.kk) || "";
    document.getElementById("download").setAttribute("href", m.url);
    document.getElementById("release").hidden = false;
    document.getElementById("fallback").hidden = true;
  }

  fetch("./latest.json", { cache: "no-store" })
    .then(function (r) { if (!r.ok) { throw new Error("http " + r.status); } return r.json(); })
    .then(function (m) {
      if (!m || !m.url || !m.versionName) { throw new Error("bad manifest"); }
      show(m);
    })
    .catch(function () {
      document.getElementById("fallback").hidden = false;
      document.getElementById("release").hidden = true;
    });
})();
