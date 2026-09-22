/* 本地静态夹具的共用脚本：URL 参数、会话内购物车、大小写。 */
(function () {
  function param(name) {
    var params = new URLSearchParams(window.location.search);
    return params.get(name) || "";
  }

  function titleCase(value) {
    if (!value) {
      return "";
    }
    return value.charAt(0).toUpperCase() + value.slice(1);
  }

  function readCart() {
    try {
      return JSON.parse(window.sessionStorage.getItem("demo-cart") || "null");
    } catch (error) {
      return null;
    }
  }

  function writeCart(cart) {
    window.sessionStorage.setItem("demo-cart", JSON.stringify(cart));
  }

  window.DemoSite = {
    param: param,
    titleCase: titleCase,
    readCart: readCart,
    writeCart: writeCart
  };
})();
