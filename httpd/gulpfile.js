var source = require('vinyl-source-stream');
var gulp = require('gulp');
var browserify = require('browserify');
var reactify = require('reactify');
var uglify = require('gulp-uglify')
var rename = require('gulp-rename');
 
var sourcesDir = './templates/';
var appEntryPoint = "_feed.jsx";
var targetDir = './static/js';
 
 
gulp.task('default', function() {
  return buildjs();
});

gulp.task('release', function() {
  buildjs();
  return gulp.src('static/js/bundle.js')
    .pipe(uglify())
    .pipe(rename('bundle.min.js'))
    .pipe(gulp.dest(targetDir));
});

function buildjs(release) {
  return browserify({entries: [sourcesDir + '/' + appEntryPoint], debug: false})
    .transform(reactify)
    .bundle()
    .pipe(source(appEntryPoint))
    .pipe(rename('bundle.js'))
    .pipe(gulp.dest(targetDir))
}
 
gulp.task('watch', function() {
  buildjs();
  gulp.watch(sourcesDir + '/' + "*.jsx", ['default']);
});
