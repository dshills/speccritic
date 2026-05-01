(function () {
  "use strict";

  // Supports the small HTMX-style subset used by this app: hx-post, hx-get,
  // hx-target, and data-modal-target.
  function submitForm(form) {
    if (specFileInput(form) && !formHasSelectedFile(form)) {
      updateSubmitAvailability(form);
      return;
    }
    var method = form.getAttribute("hx-post") ? "POST" : "GET";
    var url = form.getAttribute("hx-post") || form.getAttribute("hx-get") || form.action;
    var targetSelector = form.getAttribute("hx-target");
    var target = queryTarget(targetSelector);
    var body = form instanceof HTMLFormElement ? new FormData(form) : new FormData();
    var options = {
      method: method,
      credentials: "same-origin",
      headers: { "HX-Request": "true" }
    };
    if (method === "GET") {
      var nextURL = new URL(url, window.location.href);
      new URLSearchParams(body).forEach(function (value, key) {
        nextURL.searchParams.set(key, value);
      });
      url = nextURL.toString();
    } else {
      options.body = body;
    }
    var started = Date.now();
    var timer = startRunningState(form, target, started);

    fetch(url, options).then(function (response) {
      return response.text().then(function (text) {
        if (!response.ok) {
          throw new Error(text || response.statusText || "Request failed.");
        }
        return text;
      });
    }).then(function (text) {
      if (target) {
        if (replaceTarget(target, text)) {
          showDoneState(target, started);
        }
      }
    }).catch(function (error) {
      if (target) {
        showFailedState(target, started, error.message);
      }
    }).finally(function () {
      stopRunningState(form, timer);
    });
  }

  function loadLink(link, target) {
    return fetch(link.getAttribute("hx-get"), {
      method: "GET",
      credentials: "same-origin",
      headers: { "HX-Request": "true" }
    }).then(function (response) {
      return response.text().then(function (text) {
        if (!response.ok) {
          throw new Error(text || response.statusText || "Request failed.");
        }
        return text;
      });
    }).then(function (text) {
      if (target) {
        return replaceTarget(target, text);
      }
      return false;
    }).catch(function (error) {
      if (target) {
        showFailedMessage(target, error.message);
      }
      return false;
    });
  }

  function startRunningState(form, target, started) {
    var button = form.querySelector('button[type="submit"]');
    if (button) {
      if (!button.dataset.idleLabel) {
        button.dataset.idleLabel = button.textContent;
      }
      button.textContent = button.dataset.runningLabel || "Checking...";
      button.disabled = true;
    }
    form.setAttribute("aria-busy", "true");
    if (target) {
      target.replaceChildren(runningNode("Checking", started));
    }
    return window.setInterval(function () {
      var timer = target ? target.querySelector("[data-run-timer]") : null;
      if (timer) {
        timer.textContent = elapsedText(started);
      }
    }, 250);
  }

  function stopRunningState(form, timer) {
    window.clearInterval(timer);
    form.removeAttribute("aria-busy");
    var button = form.querySelector('button[type="submit"]');
    if (button) {
      button.disabled = false;
      button.textContent = button.dataset.idleLabel;
    }
    updateSubmitAvailability(form);
  }

  function runningNode(label, started) {
    var wrap = document.createElement("div");
    wrap.className = "check-status check-status-running";
    wrap.setAttribute("role", "status");
    wrap.setAttribute("aria-live", "polite");

    var spinner = document.createElement("span");
    spinner.className = "spinner";
    spinner.setAttribute("aria-hidden", "true");

    var text = document.createElement("span");
    text.textContent = label + " ";

    var timer = document.createElement("span");
    timer.dataset.runTimer = "true";
    timer.textContent = elapsedText(started);

    wrap.append(spinner, text, timer);
    return wrap;
  }

  function showDoneState(target, started) {
    var done = document.createElement("div");
    done.className = "check-status check-status-done";
    done.setAttribute("role", "status");
    done.setAttribute("aria-live", "polite");
    done.textContent = "Completed in " + elapsedText(started) + ".";
    target.prepend(done);
  }

  function showFailedState(target, started, message) {
    showFailedMessage(target, "Request failed after " + elapsedText(started) + ".", message);
  }

  function showFailedMessage(target, prefix, detail) {
    var message = prefix;
    if (detail) {
      message += " " + detail;
    }
    target.replaceChildren(statusNode("check-status check-status-failed", message));
  }

  function elapsedText(started) {
    return ((Date.now() - started) / 1000).toFixed(1) + "s";
  }

  function replaceTarget(target, text) {
    if (window.DOMPurify) {
      target.replaceChildren(window.DOMPurify.sanitize(text, {
        ADD_ATTR: ["hx-get", "hx-post", "hx-target", "data-modal-target"],
        RETURN_DOM_FRAGMENT: true
      }));
      return true;
    }
    target.replaceChildren(statusNode(
      "check-status check-status-failed",
      "Unable to render response because the sanitizer did not load. Refresh the page and try again."
    ));
    return false;
  }

  function statusNode(className, message) {
    var node = document.createElement("div");
    node.className = className;
    node.setAttribute("role", "status");
    node.setAttribute("aria-live", "polite");
    node.textContent = message;
    return node;
  }

  function queryTarget(selector) {
    if (!selector) {
      return null;
    }
    try {
      return document.querySelector(selector);
    } catch (e) {
      return null;
    }
  }

  function openModal(selector) {
    var modal = queryTarget(selector);
    if (!modal) {
      return;
    }
    modal.hidden = false;
    document.body.classList.add("modal-open");
    var close = modal.querySelector("[data-modal-close]");
    if (close) {
      close.focus();
    }
  }

  function closeModal(modal) {
    if (!modal) {
      return;
    }
    modal.hidden = true;
    document.body.classList.remove("modal-open");
  }

  function formHasSelectedFile(form) {
    var file = specFileInput(form);
    return !!(file && file.files && file.files.length > 0);
  }

  function specFileInput(form) {
    return form.querySelector('input[type="file"][name="spec_file"]');
  }

  function updateSubmitAvailability(form) {
    if (!specFileInput(form)) {
      return;
    }
    var button = form.querySelector('button[type="submit"]');
    if (!button || form.getAttribute("aria-busy") === "true") {
      return;
    }
    button.disabled = !formHasSelectedFile(form);
  }

  function initializeUploadForms() {
    document.querySelectorAll("form").forEach(function (form) {
      if (!form.getAttribute || !(form.getAttribute("hx-post") || form.getAttribute("hx-get"))) {
        return;
      }
      updateSubmitAvailability(form);
    });
  }

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || !form.getAttribute || !(form.getAttribute("hx-post") || form.getAttribute("hx-get"))) {
      return;
    }
    event.preventDefault();
    submitForm(form);
  });

  document.addEventListener("change", function (event) {
    var input = event.target;
    if (!input || !input.matches || !input.matches('input[type="file"][name="spec_file"]')) {
      return;
    }
    var form = input.form;
    if (form) {
      updateSubmitAvailability(form);
    }
  });

  document.addEventListener("click", function (event) {
    var link = event.target.closest ? event.target.closest("[hx-get]") : null;
    if (!link) {
      return;
    }
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) {
      return;
    }
    event.preventDefault();
    var target = queryTarget(link.getAttribute("hx-target"));
    loadLink(link, target).then(function () {
      if (link.getAttribute("data-modal-target")) {
        openModal(link.getAttribute("data-modal-target"));
      }
    });
  });

  document.addEventListener("click", function (event) {
    var close = event.target.closest ? event.target.closest("[data-modal-close]") : null;
    if (close) {
      event.preventDefault();
      closeModal(close.closest("[data-modal]"));
      return;
    }
    if (event.target.matches && event.target.matches("[data-modal]")) {
      closeModal(event.target);
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") {
      return;
    }
    var modal = document.querySelector("[data-modal]:not([hidden])");
    if (modal) {
      closeModal(modal);
    }
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initializeUploadForms);
  } else {
    initializeUploadForms();
  }
})();
