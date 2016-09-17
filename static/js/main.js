var Timer = React.createClass({
    getInitialState: function() {
        return {
            secs: this.props.secs,
            timer: null
        };
    },
    onTick: function() {
        console.log(this.state.secs - 1);
        this.setState({
            secs: this.state.secs - 1
        });
    },
    componentDidMount: function() {
        var timer = setInterval(this.onTick, 1000);
        this.setState({ timer: timer });
    },
    componentWillUnmount: function() {
        if (this.state.timer) {
            clearInterval(this.state.timer);
        }
    },
    componentWillReceiveProps: function(nextProps) {
        clearInterval(this.state.timer);
        this.setState({
            secs: this.props.secs
        });
        var timer = setInterval(this.onTick, 1000);
        this.setState({ timer: timer });
    },
    render: function() {
        var current = this.state.secs
        var hours = Math.floor(this.state.secs / 3600);
        current = current % 3600;

        var minutes = Math.floor(current / 60);
        current = current % 60;

        var secs = current;
        return (
            <span>{hours}:{minutes}:{secs}</span>
        );
    }
});

var Tile = React.createClass({
    getInitialState: function() {
        return {
            message: "",
        };
    },
    onChange: function(event) {
        this.setState({message: event.target.value});
    },
    dataStates: {
        "LOCKED_BY_CURRENT_USER": "Locked by current user",
        "LOCKED_BY_OTHER": "Locked by other",
        "PURCHASED": "Purchased",
        "OPEN": "Open"
    },
    onArrowClicked: function() {
        this.props.onArrowClicked(this.props.idx);
    },
    render: function() {
        if (this.props.dataState == 'OPEN') {

            var nextBtnClasses = "next-btn glyphicon glyphicon-play";
            if (this.state.message.length === 0) {
                nextBtnClasses += ' hide-text';
            }
            return (
                <div className="tile">
                    <span onClick={this.onArrowClicked} className={nextBtnClasses} aria-hidden="true"></span>
                    <div className="header text-center">
                        AVAILABLE
                    </div>
                    <div className="body text-center">
                        <textarea className="text-center" value={this.state.message} type="text" onChange={this.onChange} />
                    </div>
                </div>
            );
        } else if (this.props.dataState == 'LOCKED_BY_CURRENT_USER') {

            var nextBtnClasses = "next-btn glyphicon glyphicon-play";
            var qrCode = "https://chart.googleapis.com/chart?chs=95x95&cht=qr&chl=" + this.props.address;
            if (this.balance == 0) {
                nextBtnClasses += ' hide-text';
            }
            return (
                <div className="tile">
                    <span onClick={this.onArrowClicked} className={nextBtnClasses} aria-hidden="true"></span>
                    <div className="header text-center">
                       LOCKED FOR <Timer secs={this.props.ttl} />
                    </div>
                    <div className="body text-center">
                       <h3>SCAN QR CODE</h3>
                       <img className="center-block" src={qrCode} />
                    </div>
                </div>
            );

        }
    }
});

var MainComponent = React.createClass({
  getInitialState: function() {
      return { addresses: [], tiles: [], balance: null };
  },
  reloadAddresses: function() {
      var self = this;
      var addressesRequest = $.getJSON('/addresses');
      var tilesRequest = $.getJSON('/tiles');
      $.when(addressesRequest, tilesRequest).then(function(a, b) {
          self.setState({
              addresses: a[0],
              tiles: b[0],
              balance: a[0][0].balance,
          });
      });
  },
  componentDidMount: function() {
      var self = this;
      this.reloadAddresses();
      setInterval(function() {
          self.reloadAddresses();
      }, 3000);
  },
  lockTable: function(idx) {
      var self = this;
      $.post("/tile", JSON.stringify({
          "frame_number": idx
      }), function(res) {
          debugger;
          self.reloadAddresses();
      });
  },
  render: function() {
    var tiles = [];
    for (var i=0; i < this.state.addresses.length; i++) {
        var balance = this.state.balance;
        var address = this.state.addresses[i].address;
        var tileData = this.state.tiles[i];
        var tile = (
            <div key={i} className="col-md-2 col-sm-2">
                <Tile
                 idx={i}
                 onArrowClicked={this.lockTable}
                 dataState={tileData.state}
                 ttl={tileData.ttl}
                 address={address}
                 message={tileData.message}
                 balance={balance} />
            </div>
        );
        tiles.push(tile);
    }

    return (
      <div>
         {tiles}
      </div>
    );
  }
});

ReactDOM.render(
  <MainComponent />,
  document.getElementsByClassName('main')[0]
);
