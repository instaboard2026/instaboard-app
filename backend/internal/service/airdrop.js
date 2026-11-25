ObjC.import('AppKit');
var done = false, result = 'timeout';
ObjC.registerSubclass({
  name: 'IBAirDropDelegate',
  protocols: ['NSSharingServiceDelegate'],
  methods: {
    'sharingService:didShareItems:': { types: ['void', ['id', 'id']], implementation: function () { result = 'shared'; done = true; } },
    'sharingService:didFailToShareItems:error:': { types: ['void', ['id', 'id', 'id']], implementation: function (s, i, e) { result = 'failed:' + ObjC.unwrap(e.localizedDescription); done = true; } },
  },
});
function run(argv) {
  const app = $.NSApplication.sharedApplication;
  app.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
  const svc = $.NSSharingService.sharingServiceNamed($.NSSharingServiceNameSendViaAirDrop);
  if (svc.isNil()) return 'unavailable';
  const items = $(argv.map(function (p) { return $.NSURL.fileURLWithPath(p); }));
  if (!svc.canPerformWithItems(items)) return 'unavailable';
  const delegate = $.IBAirDropDelegate.alloc.init;
  svc.delegate = delegate;
  app.activateIgnoringOtherApps(true);
  svc.performWithItems(items);
  const deadline = Date.now() + 30 * 60 * 1000;
  while (!done && Date.now() < deadline) {
    $.NSRunLoop.currentRunLoop.runUntilDate($.NSDate.dateWithTimeIntervalSinceNow(0.25));
  }
  return result;
}
