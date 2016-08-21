var Tile = React.createClass({
    lock: function() {
        $.post("/tile", JSON.stringify({
            "frame_number": this.props.idx
        }), function(res) {
            console.log(res);
        });
    },
    getInitialState: function() {
        return {
            message: ""
        };
    },
    purchase: function(event) {
        if (this.state.message.length == 0) {
            return;
        }
        $.post("/purchase", JSON.stringify({
            "frame_number": this.props.idx,
            "message": this.state.message
        }), function(res) {
            debugger;
            this.setState({message: ""});
        });
    },
    onChange: function(event) {
        this.setState({message: event.target.value});
    },
    render: function() {
        var src = "https://chart.googleapis.com/chart?chs=250x250&cht=qr&chl=" + this.props.address.address;
        var additional;
        if (this.props.data.state == "LOCKED_BY_CURRENT_USER") {
            additional = (
                <div>
                    <input type="text" value={this.state.message} onChange={this.onChange} />
                    <button onClick={this.purchase}>Purchase</button>
                </div>
            );
        } else if (["PURCHASED", "LOCKED_BY_OTHER"].indexOf(this.props.data.state) != -1) {
            additional = "";
        } else {
            additional = (
                <button onClick={this.lock}>Lock</button>
            );
        }
        return (
            <div>
                <img key={this.props.address.address} src={src} />
                <p>{this.props.address.balance} BTC deposited</p>
                <p>Message: {this.props.data.message}</p>
                <p>State: {this.props.data.state}</p>
                {additional}
            </div>
        );
    }
});

var MainComponent = React.createClass({
  getInitialState: function() {
      return { addresses: [], tiles: [] };
  },
  componentDidMount: function() {
      var self = this;
      setInterval(function() {
          var addressesRequest = $.getJSON('/addresses');
          var tilesRequest = $.getJSON('/tiles');
          $.when(addressesRequest, tilesRequest).then(function(a, b) {
              self.setState({
                  addresses: a[0],
                  tiles: b[0]
              });
          });
      }, 3000);
  },
  render: function() {
    var res = [];
    for (var i=0; i < this.state.addresses.length; i++) {
        res.push(
            <li><Tile idx={i} data={this.state.tiles[i]} address={this.state.addresses[i]} /></li>
        );
    }
    return (
      <ul>{res}</ul>
    );
  }
});

ReactDOM.render(
  <MainComponent />,
  document.getElementsByClassName('container')[0]
);
