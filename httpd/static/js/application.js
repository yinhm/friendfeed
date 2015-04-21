
$(document).ready(function() {
  jQuery.fn.formToDict = function() {
    var fields = this.serializeArray();
    var json = {}
    for (var i = 0; i < fields.length; i++) {
      json[fields[i].name] = fields[i].value;
    }
    return json;
  };

  jQuery.postJSON = function(url, data, callback) {
    $.post(url, data, callback, "json");
  };

  function makeCommentDiv(comment) {
    var commentDiv = $('<div class="comment"></div>');
    commentDiv.append(comment.body);
    if (comment.from) {
      var from = $('<a href="/feed/' + comment.from.id + '"/>');
      from.text(comment.from.name);
      commentDiv.append(' - ');
      commentDiv.append(from);
    }
    if (comment.date) {
      commentDiv.attr("title", comment.date);
    }
    return commentDiv;
  }

  $("div").on("click", ".commentcommand", function() {
    var entry = $(this).parents(".entry");
    var existing = entry.find(".comment.form");
    if (existing.length > 0) {
      existing.find("input[name=body]").focus();
      return false;
    }
    var form = $('<div class="comment form"><form method="post"><input type="text" name="body" style="width:300px"/> <input type="submit" value="Comment"/></form></div>');
    form.find("form").submit(function() {
      form.find("input[type=submit]").attr("disabled", "disabled");
      var args = $.extend({entry: entry.attr("eid")},
                          form.find("form").formToDict());
      $.postJSON("/a/comment", args, function(comment) {
        form.remove();
        entry.find(".body").append(makeCommentDiv(comment));
      });
      return false;
    });
    entry.find(".body").append(form);
    form.find("input[name=body]").focus();
    return false;
  });

  $("div").on("click", ".likecommand", function() {
    var link = $(this);
    var entry = link.parents(".entry");
    $.postJSON("/a/like", {entry: entry.attr("eid")}, function(like) {
      link.removeClass("likecommand").addClass("unlikecommand").html("Unlike");
    });
    return false;
  });

  $("div").on("click", ".unlikecommand", function() {
    var link = $(this);
    var entry = link.parents(".entry");
    $.postJSON("/a/like/delete", {entry: entry.attr("eid")}, function(response) {
      link.removeClass("unlikecommand").addClass("likecommand").html("like");
    }); 
    return false;
  });

  $("div").on("click", ".hidecommand", function() {
    var entry = $(this).parents(".entry");
    $.postJSON("/a/hide", {entry: entry.attr("eid")}, function(response) {
      entry.attr("oldhtml", entry.html());
      entry.html('<span style="font-style: italic">Entry hidden</span> - <a href="#" class="unhidecommand">undo</a>.');
    });
  });

  $("div").on("click", ".unhidecommand", function() {
    var entry = $(this).parents(".entry");
    $.postJSON("/a/unhide", {entry: entry.attr("eid")}, function(response) {
      entry.html(entry.attr("oldhtml"));
    });
  });

  $("div").on("click", ".expandcomments", function() {
    var entry = $(this).parents(".entry");
    var eid = $(this).parents(".entry").attr("eid") || $(this).parents(".entry").attr("data-eid");
    $.getJSON("/a/entry/" + eid, function(data) {
      entry.find(".comment").remove();
      $.each(data.comments, function(i, comment) {
        entry.find(".body").append(makeCommentDiv(comment));
      });
    });
    return false;
  });
});
