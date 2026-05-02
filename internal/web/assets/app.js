(function () {
  "use strict";

  // Supports the small HTMX-style subset used by this app: hx-post, hx-get,
  // hx-target, and data-modal-target.
  function submitForm(form) {
    hideModelMenu(form);
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
    setFormControlsDisabled(form, true);
    var button = form.querySelector('button[type="submit"]');
    if (button) {
      if (!button.dataset.idleLabel) {
        button.dataset.idleLabel = button.textContent;
      }
      button.textContent = button.dataset.runningLabel || "Checking...";
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
    setFormControlsDisabled(form, false);
    var button = form.querySelector('button[type="submit"]');
    if (button) {
      button.textContent = button.dataset.idleLabel;
    }
    updateSubmitAvailability(form);
  }

  function setFormControlsDisabled(form, disabled) {
    if (!form) {
      return;
    }
    Array.prototype.forEach.call(form.querySelectorAll("input:not([type='hidden']), select, textarea, button"), function (control) {
      if (disabled) {
        if (!control.disabled) {
          control.dataset.runningDisabled = "true";
          control.disabled = true;
        }
        return;
      }
      if (control.dataset.runningDisabled === "true") {
        control.disabled = false;
        delete control.dataset.runningDisabled;
      }
    });
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
      initializeModelPicker(form);
      updateSubmitAvailability(form);
    });
  }

  function initializeModelPicker(form) {
    var provider = form.querySelector('select[name="llm_provider"]');
    var model = form.querySelector('input[name="llm_model"]');
    if (!provider || !model) {
      return;
    }
    model.dataset.lastProviderDefault = selectedProviderDefault(provider);
    loadProviderModels(form);
  }

  function selectedProviderDefault(provider) {
    var selected = provider.options[provider.selectedIndex];
    return selected ? selected.getAttribute("data-default-model") || "" : "";
  }

  function loadProviderModels(form) {
    var provider = form.querySelector('select[name="llm_provider"]');
    var model = form.querySelector('input[name="llm_model"]');
    var status = form.querySelector("#model_picker_status");
    if (!provider || !model) {
      return;
    }
    var fallbackDefault = selectedProviderDefault(provider);
    var requestID = String(Date.now()) + ":" + provider.value;
    form.dataset.modelRequestId = requestID;
    if (form.modelRequestController) {
      form.modelRequestController.abort();
    }
    var controller = window.AbortController ? new AbortController() : null;
    form.modelRequestController = controller;
    if (status) {
      status.textContent = "Loading available models...";
    }
    var options = {
      method: "GET",
      credentials: "same-origin",
      headers: { "Accept": "application/json" }
    };
    if (controller) {
      options.signal = controller.signal;
    }
    fetch("/models?provider=" + encodeURIComponent(provider.value), options).then(function (response) {
      return response.json().then(function (payload) {
        if (!response.ok) {
          throw new Error(payload && payload.error ? payload.error : response.statusText);
        }
        return payload;
      });
    }).then(function (payload) {
      if (form.dataset.modelRequestId !== requestID) {
        return;
      }
      var models = Array.isArray(payload.models) ? payload.models : [];
      form.modelOptions = models;
      var nextDefault = payload.default_model || fallbackDefault;
      model.dataset.lastProviderDefault = nextDefault || fallbackDefault;
      if (status) {
        status.textContent = models.length ? models.length + " models loaded." : "No models returned; enter a model manually.";
      }
    }).catch(function (error) {
      if (error && error.name === "AbortError") {
        return;
      }
      if (form.dataset.modelRequestId !== requestID) {
        return;
      }
      model.dataset.lastProviderDefault = fallbackDefault;
      if (status) {
        status.textContent = "Could not load models; enter a model manually.";
      }
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

  document.addEventListener("change", function (event) {
    var provider = event.target;
    if (!provider || !provider.matches || !provider.matches('select[name="llm_provider"]')) {
      return;
    }
    var form = provider.form;
    var model = form ? form.querySelector('input[name="llm_model"]') : null;
    if (!model) {
      return;
    }
    var nextDefault = selectedProviderDefault(provider);
    model.value = "";
    model.dataset.lastProviderDefault = nextDefault;
    loadProviderModels(form);
    showModelMenu(form, "");
  });

  document.addEventListener("focusin", function (event) {
    var model = event.target;
    if (!model || !model.matches || !model.matches('input[name="llm_model"]')) {
      return;
    }
    showModelMenu(model.form, "");
  });

  document.addEventListener("input", function (event) {
    var model = event.target;
    if (!model || !model.matches || !model.matches('input[name="llm_model"]')) {
      return;
    }
    showModelMenu(model.form, model.value);
  });

  document.addEventListener("mousedown", function (event) {
    var option = event.target.closest ? event.target.closest("[data-model-option]") : null;
    if (!option) {
      return;
    }
    event.preventDefault();
  });

  document.addEventListener("click", function (event) {
    var option = event.target.closest ? event.target.closest("[data-model-option]") : null;
    if (option) {
      var form = option.closest("form");
      var model = form ? form.querySelector('input[name="llm_model"]') : null;
      if (model) {
        model.value = option.getAttribute("data-model-option") || "";
        model.focus();
      }
      hideModelMenu(form);
      return;
    }
    if (!event.target.closest || !event.target.closest(".model-picker")) {
      document.querySelectorAll("form").forEach(hideModelMenu);
    }
  });

  function showModelMenu(form, filter) {
    var menu = form ? form.querySelector("#model_options") : null;
    var model = form ? form.querySelector('input[name="llm_model"]') : null;
    if (!menu || !model) {
      return;
    }
    var models = Array.isArray(form.modelOptions) ? form.modelOptions : [];
    var needle = (filter || "").trim().toLowerCase();
    var visible = models.filter(function (item) {
      if (!item || !item.id) {
        return false;
      }
      if (!needle) {
        return true;
      }
      return item.id.toLowerCase().indexOf(needle) >= 0 ||
        (item.display_name || "").toLowerCase().indexOf(needle) >= 0;
    });
    menu.replaceChildren();
    visible.forEach(function (item) {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "model-option";
      button.setAttribute("role", "option");
      button.setAttribute("data-model-option", item.id);
      var id = document.createElement("span");
      id.textContent = item.id;
      button.append(id);
      if (item.display_name) {
        var name = document.createElement("small");
        name.textContent = item.display_name;
        button.append(name);
      }
      menu.append(button);
    });
    menu.hidden = visible.length === 0;
    model.setAttribute("aria-expanded", menu.hidden ? "false" : "true");
  }

  function hideModelMenu(form) {
    var menu = form ? form.querySelector("#model_options") : null;
    var model = form ? form.querySelector('input[name="llm_model"]') : null;
    if (menu) {
      menu.hidden = true;
    }
    if (model) {
      model.setAttribute("aria-expanded", "false");
    }
  }

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
